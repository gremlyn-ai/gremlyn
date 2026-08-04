---
name: stack-health
description: Full health check of the local Gremlyn stack — the single module's build state, shield/arena/dashboard services, SQLite databases, optional Postgres/Redis/ML sidecar, and the end-to-end proxy against a real MCP server. Use when the user says "stack health", "check the stack", "is everything running", "health check", "doctor", or runs /stack-health.
---

# Stack Health Check

You are a senior engineer auditing the local Gremlyn development stack. Gremlyn is **local-first** — there is no production to monitor, so this check covers the developer's machine and the one repo.

## Usage

```
/stack-health              # full audit
/stack-health build        # repo status + build gate only
/stack-health services     # running services only
/stack-health db           # databases only
/stack-health e2e          # proxy against a real MCP server only
```

Run the independent checks **in parallel** where possible.

---

## Step 0 — Repo layout

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
git rev-parse --show-toplevel
head -1 go.mod
ls -d cmd/* pkg/* internal/* migrations/* dashboard docs
```

Expected: **one** git repo rooted here, **one** Go module `github.com/gremlyn-ai/gremlyn`, holding `cmd/{gremlyn,shield,arena}`, `pkg/*`, `internal/{cli,shield,arena}`, `migrations/{shield,arena}`, `dashboard/`, `docs/`. The four old repos were merged on 2026-08-04; they survive only as a backup at `../gremlyn-old-repos-backup/` and are **not** part of the stack.

---

## Step 1 — Repo status

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
echo "branch=$(git rev-parse --abbrev-ref HEAD)  HEAD=$(git log -1 --format='%h %s')"
git status --short | head -20
find . -name go.mod -not -path './dashboard/node_modules/*'
grep -n '^replace' go.mod || echo "no replace directives (correct)"
```

**Findings**:
- More than one `go.mod` → 🔴 the module got split again; the merge is being undone by accident.
- Any `replace` directive → 🔴. There is nothing left to replace, and a local-path replace breaks every fresh clone.
- Uncommitted changes → informational, but **say so**: everything below tests the working tree, not `HEAD`.
- **Version skew is no longer a possible failure class.** One module, one commit, one version across the CLI, both services and the shared engine. Do not look for it and do not report it.

---

## Step 2 — Build & gate

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
go build ./... 2>&1 | head -20
go vet ./... 2>&1 | head -20
```

Then the real gate (slower — run if the user wants depth):
```bash
make check 2>&1 | tail -20          # vet + lint + go test -race
make build && ls -lh bin/           # → bin/{gremlyn,shield,arena}
```

**Thresholds**: build failure = 🔴 CRITICAL — and note that it now takes **all three binaries** down at once. That is the trade the merge made: no skew to diagnose, but no partially-green stack either. `go vet` output = 🟠. A `-race` failure inside `make check` = 🔴 and **never** a flake.

### CGO check
```bash
CGO_ENABLED=0 go build -o /tmp/gremlyn-cgotest ./cmd/gremlyn && echo "CGO_ENABLED=0 OK"
```
Failure = 🔴 CRITICAL — static cross-compiled binaries are how this ships, and pure-Go `modernc.org/sqlite` is why it works today. The cause is almost always a newly added dependency pulling in C. `make build` already sets `CGO_ENABLED=0`, so a green `make build` covers all three binaries; run the explicit check when the dependency graph moved.

---

## Step 3 — Dashboard

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn/dashboard
npm run typecheck 2>&1 | tail -20
npm run lint 2>&1 | tail -10
```

Full frontend gate, from the repo root: `make dashboard-check` (typecheck + lint + vitest + build).

**Thresholds**: any `tsc` error = 🔴. A type error with no local dashboard change usually means the **Go DTOs moved and `dashboard/lib/api/types.ts` wasn't updated** — that's the Go↔TS contract detector doing its job. It is now a same-commit fix: the change that moved the DTO owns the type update.

---

## Step 4 — Running services

```bash
curl -fsS -o /dev/null -w "shield    HTTP %{http_code} in %{time_total}s\n" localhost:8081/api/v1/health || echo "shield    DOWN"
curl -fsS -o /dev/null -w "arena     HTTP %{http_code} in %{time_total}s\n" localhost:8082/api/v1/health || echo "arena     DOWN"
curl -fsS -o /dev/null -w "dashboard HTTP %{http_code} in %{time_total}s\n" localhost:3000 || echo "dashboard DOWN"
ss -ltnp 2>/dev/null | grep -E ':(8081|8082|3000)' || true
```

**Thresholds**: non-2xx on a service the user expected up = 🔴. > 2s = 🟠 degraded. Down is **normal** if they simply haven't started it — ask rather than alarm.

To bring the stack up (3 terminals, from the repo root):
```bash
make build
./bin/shield                    # :8081
./bin/arena                     # :8082
cd dashboard && npm run dev     # :3000
```

**Check the independence property**: Shield down must not blank Arena's dashboard pages. If it does, that's a 🔴 UI bug worth reporting.

---

## Step 5 — SQLite databases (the default store)

```bash
ls -lh ~/.gremlyn/ 2>/dev/null || echo "~/.gremlyn does not exist yet (fresh install)"

for db in shield arena; do
  f="$HOME/.gremlyn/$db.db"
  [ -f "$f" ] || { echo "$db: no DB yet"; continue; }
  echo "=== $db  $(du -h "$f" | cut -f1)  modified $(date -d "@$(stat -c %Y "$f")" '+%F %T')"
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
- `journal_mode` not `wal` = 🟠 (the code sets `PRAGMA journal_mode=WAL` on open — if it reports otherwise, something opened this file wrong, and concurrency will suffer)
- **DB > 1 GB** = 🟠 → retention isn't pruning. This is the predictable failure of an inline proxy that writes per message, and it arrives quietly. Route to `database-engineer`
- Stale `-wal`/`-shm` with no live process = informational; SQLite recovers it. **Never delete a `-wal` with a live writer** (`pgrep -af 'bin/(shield|arena)'` before you even think about it)
- No DB at all = fine, fresh install

**Before suggesting any deletion**: `rm ~/.gremlyn/*.db` destroys the user's event history and every recorded Arena session — permanently, with no export. Say that in those words and get explicit confirmation. Never run it as part of a health check.

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

The only check that proves the product works. **Do not skip it and do not accept "the process started" as a pass** — `gremlyn wrap` shipped broken for months precisely because nothing exercised the data path.

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
make build
./bin/gremlyn version
./bin/gremlyn doctor
./bin/gremlyn status
```

Then the real wire test:
```bash
./bin/gremlyn wrap -- npx -y @modelcontextprotocol/server-memory
```

Framing is **newline-delimited JSON — one object per line, no `Content-Length` header**. Feed these on stdin and require a response back for each, with a matching `id`:
```
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"stack-health","version":"0"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
```
`initialize` **and** `tools/list` must both round-trip. Then close stdin: the proxy must half-close (child stdin closed, responses still drained) and exit cleanly, not kill the child mid-response.

**Thresholds**:
- `gremlyn version` not reporting an injected version = 🟠 (ldflags stamping broken → unsupportable in the field)
- An explicit `peer is using LSP Content-Length framing` error = 🔴 framing regression in `pkg/protocol`; the diagnostic already tells you which side is wrong
- A request forwarded with **no response coming back**, or a response lost when stdin closes = 🔴 teardown regression in `pkg/proxy` (the shared-shutdown-channel bug, back)
- Wrap mode failing at all = 🔴 CRITICAL, the core product is broken
- `doctor` reporting a problem = follow its guidance

---

## Step 8 — Resource sanity

```bash
pgrep -af 'bin/(shield|arena)|next-server' || true
df -h ~ | tail -1
```

Look for: orphaned `shield`/`arena` processes from a previous run holding a port or the SQLite write lock (a very common cause of "it won't start"), leftover `gremlyn wrap` children from a killed session, and disk pressure (event DBs grow).

---

## Report Format

```markdown
# Gremlyn Stack Health — <date>

## Verdict
🟢 healthy | 🟡 degraded | 🔴 broken

## Repo & build
- Branch `<name>` · HEAD `<sha> <subject>` · dirty: <n files>
- Single module `github.com/gremlyn-ai/gremlyn`, no `replace`: ✅/🔴
- `go build ./...` / `go vet ./...`: ✅/🟠/🔴
- `make check` (vet + lint + test -race): ✅/🔴
- `CGO_ENABLED=0`: ✅/🔴
- `bin/{gremlyn,shield,arena}`: ✅/🔴
- `make dashboard-check`: ✅/🔴

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
- wrap + real MCP server: `initialize` ✅/🔴 · `tools/list` ✅/🔴 · clean half-close ✅/🔴

## Findings
### 🔴 Critical
### 🟠 Warnings
### 🟢 OK

## Recommended actions
1. <action> — <exact command, and where it runs>
```

## Rules

- **Down ≠ broken.** A service the user simply hasn't started is not a finding. Ask.
- **All optional services down is the correct default state.** Never report it as a problem.
- **Version skew is not a failure class here.** One module, one version. Never offer it as an explanation.
- **Always give the exact command and where it runs** — repo root or `dashboard/`.
- **Warn before anything destructive** — deleting a DB deletes the user's history, irreversibly.
- **A green build is not a working product.** Step 7 is the one that proves it; both bugs that made `wrap` useless passed every build.
- A silent fail-open (a dependency down and traffic passing unchecked with no event) is the single most serious finding this check can produce. Escalate it as 🔴 regardless of anything else being green.
