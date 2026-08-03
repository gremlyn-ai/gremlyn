---
name: stack-health
description: Full health check of the local Gremlyn stack — the four repos' build state, shield/arena/dashboard services, SQLite databases, optional Postgres/Redis/ML sidecar, cross-repo version alignment, and the end-to-end proxy against a real MCP server. Use when the user says "stack health", "check the stack", "is everything running", "health check", "doctor", or runs /stack-health.
---

# Stack Health Check

You are a senior engineer auditing the local Gremlyn development stack. Gremlyn is **local-first** — there is no production to monitor, so this check covers the developer's machine and the four repos.

## Usage

```
/stack-health              # full audit
/stack-health repos        # build + version alignment only
/stack-health services     # running services only
/stack-health db           # databases only
/stack-health e2e          # proxy against a real MCP server only
```

Run the independent checks **in parallel** where possible.

---

## Step 0 — Repo layout

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn && ls -d gremlyn-*
```

Expected: `gremlyn-core`, `gremlyn-shield`, `gremlyn-arena`, `gremlyn-dashboard`. Four **independent** git repositories; the parent is not a repo.

---

## Step 1 — Cross-repo version alignment (the #1 source of confusing failures)

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
for d in gremlyn-core gremlyn-shield gremlyn-arena gremlyn-dashboard; do
  echo "=== $d  branch=$(git -C $d rev-parse --abbrev-ref HEAD)  $(git -C $d log -1 --format=%h)"
  git -C "$d" status --short | head -5
done

echo "--- core version pinned by consumers"
grep -H 'gremlyn-core' gremlyn-shield/go.mod gremlyn-arena/go.mod
echo "--- replace directives (expected in local dev, MUST NOT ship in a tag)"
grep -H '^replace' gremlyn-shield/go.mod gremlyn-arena/go.mod
```

**Findings**:
- A `replace … => ../gremlyn-core` present → **normal for local dev**. Note it; it must not be in a tagged release.
- Consumers pinning **different** core versions → ⚠️ WARNING, they'll behave differently.
- Uncommitted changes in core while consumers are being tested → ⚠️ the consumers are testing against unversioned code.

---

## Step 2 — Build & gate, per Go repo

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
for d in gremlyn-core gremlyn-shield gremlyn-arena; do
  echo "=== $d"
  (cd "$d" && go build ./... 2>&1 | head -20 && go vet ./... 2>&1 | head -20)
done
```

Then the real gate (slower — run if the user wants depth):
```bash
(cd gremlyn-core && make check) 2>&1 | tail -20
```

**Thresholds**: build failure = 🔴 CRITICAL. `go vet` output = 🟠. A core build failure implies both consumers are broken — check them regardless of what they report cached.

### CGO check
```bash
(cd gremlyn-core && CGO_ENABLED=0 go build -o /tmp/gremlyn-cgotest ./cmd/gremlyn && echo "CGO_ENABLED=0 OK")
```
Failure = 🔴 CRITICAL — static cross-compiled binaries are how this ships.

### Clean-clone check (only before tagging)
A `replace` pointing at `../gremlyn-core` in a tagged release breaks every fresh clone of shield/arena. Verify the build resolves core from the module proxy before a release. Harmless in dev.

---

## Step 3 — Dashboard

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn/gremlyn-dashboard
npm run typecheck 2>&1 | tail -20
npm run lint 2>&1 | tail -10
```

**Thresholds**: any `tsc` error = 🔴. A type error with no local dashboard change usually means the **Go DTOs moved and `lib/api/types.ts` wasn't updated** — that's the cross-repo contract detector doing its job.

---

## Step 4 — Running services

```bash
curl -fsS -o /dev/null -w "shield    HTTP %{http_code} in %{time_total}s\n" localhost:8081/api/v1/health || echo "shield    DOWN"
curl -fsS -o /dev/null -w "arena     HTTP %{http_code} in %{time_total}s\n" localhost:8082/api/v1/health || echo "arena     DOWN"
curl -fsS -o /dev/null -w "dashboard HTTP %{http_code} in %{time_total}s\n" localhost:3000 || echo "dashboard DOWN"
ss -ltnp 2>/dev/null | grep -E ':(8081|8082|3000)' || true
```

**Thresholds**: non-2xx on a service the user expected up = 🔴. > 2s = 🟠 degraded. Down is **normal** if they simply haven't started it — ask rather than alarm.

To bring the stack up (3 terminals):
```bash
cd gremlyn-shield    && go run ./cmd/shield      # :8081
cd gremlyn-arena     && go run ./cmd/arena       # :8082
cd gremlyn-dashboard && npm run dev              # :3000
```

**Check the independence property**: Shield down must not blank Arena's dashboard pages. If it does, that's a 🔴 UI bug worth reporting.

---

## Step 5 — SQLite databases (the default store)

```bash
ls -lh ~/.gremlyn/ 2>/dev/null || echo "~/.gremlyn does not exist yet (fresh install)"

for db in shield arena; do
  f="$HOME/.gremlyn/$db.db"
  [ -f "$f" ] || { echo "$db: no DB yet"; continue; }
  echo "=== $db  $(du -h "$f" | cut -f1)"
  sqlite3 "$f" "PRAGMA integrity_check;" | head -3
  sqlite3 "$f" "PRAGMA journal_mode;"
  sqlite3 "$f" "SELECT name FROM sqlite_master WHERE type='table' ORDER BY name;" | tr '\n' ' '; echo
done

sqlite3 ~/.gremlyn/shield.db "SELECT COUNT(*) AS events FROM events;" 2>/dev/null
sqlite3 ~/.gremlyn/arena.db  "SELECT COUNT(*) AS arena_events FROM arena_events;" 2>/dev/null
sqlite3 ~/.gremlyn/arena.db  "SELECT COUNT(*) AS sessions FROM sessions;" 2>/dev/null
```

**Thresholds**:
- `integrity_check` ≠ `ok` = 🔴 CRITICAL
- `journal_mode` not `wal` = 🟠 (concurrency will suffer)
- **DB > 1 GB** = 🟠 → retention isn't pruning. This is the predictable failure of an inline proxy that writes per message. Route to `database-engineer`
- Stale `-wal`/`-shm` with no live process = informational; SQLite recovers it. **Never delete a `-wal` with a live writer**
- No DB at all = fine, fresh install

**Before suggesting any deletion**: `rm ~/.gremlyn/*.db` destroys the user's event history. Say that explicitly and get confirmation.

---

## Step 6 — Optional services

Only if the user runs the non-default paths. **All of these being down is normal and correct** — SQLite + no Docker is the zero-config default, and that's a product feature.

```bash
docker compose ps 2>/dev/null || echo "no compose stack running (normal)"
[ -n "$DATABASE_URL" ] && psql "$DATABASE_URL" -c "SELECT version();" | head -2 || echo "DATABASE_URL unset → SQLite path (default)"
redis-cli ping 2>/dev/null || echo "redis down → rate limiting on the degraded path"
curl -fsS localhost:8000/health 2>/dev/null || echo "ML sidecar down → L2 detection on the degraded path"
```

**The check that matters here**: if Redis or the sidecar is down, confirm Shield is running on its **documented fallback and emitting a degradation event** — not silently allowing traffic unchecked. A silent fail-open is 🔴 CRITICAL.

---

## Step 7 — End-to-end: the proxy against a real MCP server

The only check that proves the product works.

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn/gremlyn-core
go build -o gremlyn ./cmd/gremlyn
./gremlyn version
./gremlyn doctor
./gremlyn status
```

Then the real wire test:
```bash
./gremlyn wrap -- npx @modelcontextprotocol/server-memory
```
Exercise `initialize`, `tools/list`, `tools/call`. The proxy must pass all three through and stay up.

**Thresholds**: `gremlyn version` not reporting an injected version = 🟠 (ldflags stamping broken, unsupportable in the field). `doctor` reporting a problem = follow its guidance. Wrap mode failing = 🔴 CRITICAL, the core product is broken.

---

## Step 8 — Resource sanity

```bash
pgrep -af 'shield|arena|next-server' || true
df -h ~ | tail -1
```

Look for: orphaned `shield`/`arena` processes from a previous run holding a port or the SQLite write lock (a very common cause of "it won't start"), and disk pressure (event DBs grow).

---

## Report Format

```markdown
# Gremlyn Stack Health — <date>

## Verdict
🟢 healthy | 🟡 degraded | 🔴 broken

## Repos
| repo | branch | HEAD | dirty | build | vet |
|---|---|---|---|---|---|

- Core version pinned: shield `<v>` · arena `<v>` — aligned? ✅/⚠️
- Replace directives present: yes (normal for dev) / no
- `CGO_ENABLED=0`: ✅/🔴

## Services
| service | port | status | latency |
|---|---|---|---|

- Independent failure verified: ✅/❌

## Databases
| db | size | integrity | journal | rows |
|---|---|---|---|---|

## Optional services
| service | status | fallback verified |
|---|---|---|

## End-to-end
- `gremlyn version`: <output>
- `gremlyn doctor`: <summary>
- wrap + real MCP server: ✅/🔴

## Findings
### 🔴 Critical
### 🟠 Warnings
### 🟢 OK

## Recommended actions
1. <action> — <which repo / which command>
```

## Rules

- **Down ≠ broken.** A service the user simply hasn't started is not a finding. Ask.
- **All optional services down is the correct default state.** Never report it as a problem.
- **Always name which repo** a command runs in.
- **Warn before anything destructive** — deleting a DB deletes the user's history.
- A silent fail-open (a dependency down and traffic passing unchecked with no event) is the single most serious finding this check can produce. Escalate it as 🔴 regardless of anything else being green.
