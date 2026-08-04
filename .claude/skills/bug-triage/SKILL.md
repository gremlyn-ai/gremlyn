---
name: bug-triage
description: Triage and fix bugs in the Gremlyn repo — classify (unclear / feature / duplicate / works-as-intended / fixable), locate the root cause, fix on a branch with a regression test, and report. Use when the user says "triage bugs", "bug manager", "fix this bug", "work the backlog", or runs /bug-triage.
---

# Bug Triage

Triage and fix bugs in Gremlyn. Works from a local backlog file, a GitHub issue, or a bug the user describes in the conversation.

## Usage

```
/bug-triage                      # work the backlog
/bug-triage <issue-number>       # one GitHub issue
/bug-triage "<description>"      # a bug described inline
```

## Step 0 — Orient: what code, and what is behind the proxy

Before reading any code. Cheap, and it stops you debugging a symptom of a dirty tree.

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
echo "branch=$(git rev-parse --abbrev-ref HEAD)  HEAD=$(git log -1 --format='%h %s')"
git status --short | head -20
./bin/gremlyn version 2>/dev/null || make build
```

One module, one commit — **there is no version skew to rule out**. What replaces that question:
- **Is the tree dirty?** Uncommitted work is the most common source of "it broke and I didn't touch anything". Name the dirty files in the report.
- **Is the binary stale?** `./bin/*` is built, not live. A bug that reproduces with `./bin/shield` but not `go run ./cmd/shield` is a stale binary, not a bug.
- **Which layer owns the symptom?** `pkg/proxy` + `pkg/protocol` are on the hot path of everything: a change there breaks the CLI, Shield and Arena in the same commit. Widen the blast radius accordingly, don't narrow it.

If the report comes from a user, get `gremlyn version` output, their MCP client config, and **which MCP servers sit behind the proxy** (name and version — a peer that speaks LSP-style framing produces a very different failure than a conforming one). If you had to ask for any of it, that's a missing `gremlyn doctor` check — note it as a follow-up.

## Step 1 — Reproduce

A bug you can't reproduce is a bug you can't verify you fixed.

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
make build

# is it in the proxy path?
./bin/gremlyn wrap -- npx -y @modelcontextprotocol/server-memory

# is it service-side?
./bin/shield                      # :8081
./bin/arena                       # :8082

# is it dashboard-side?
cd dashboard && npm run dev       # :3000
```

Check the operational causes before the code ones — they're more common than logic bugs:
- an orphaned `shield`/`arena` process holding a port or the SQLite write lock (`pgrep -af 'bin/(shield|arena)'`)
- a stale binary against a migrated DB — rebuild before believing anything
- `~/.gremlyn/` permissions, disk full
- an optional dependency down (Redis, ML sidecar) putting a layer on its degraded path

## Step 2 — Classify

Read the report, then trace the root cause in code (Grep/Read). Read `CLAUDE.md` and the relevant `docs/{core,shield,arena,dashboard}.md` first — for behavior questions, `.claude/agents/mcp-domain-expert.md` documents what the semantics are *supposed* to be, which often settles "bug or not" immediately.

| Branch | When | Action |
|--------|------|--------|
| **Unclear / no repro** | Missing steps, version, or MCP server context; can't locate the defect | Ask for the SPECIFIC missing info. Stop |
| **Actually a feature** | Requests new behavior, not a defect | Say why it's a feature, route to `product-manager`. Stop |
| **Duplicate** | Same root cause as another open item | Reference it. Stop |
| **Already fixed** | Current code does the right thing | Say what fixed it, and add the regression test if it's missing |
| **Works as intended** | Code is correct; behavior is documented | Say what you checked, cite the doc. If the docs were unclear, that's a docs bug — fix it |
| **Clean fixable bug** | Root cause found, focused fix is clear | → Step 3 |

### Gremlyn-specific classification traps

- **"Shield blocked something legitimate"** — that's a **false positive**, the expensive failure mode. It's a real bug, and the fix is a detection change with negative corpus cases (`/new-detection-rule`), never a widened pattern applied by feel.
- **"The agent hung"** — check whether Shield produced a **silent drop instead of a JSON-RPC error**. A block the agent can't see becomes a hang, and that's a bug in the block path, not the agent.
- **"The proxy forwarded a request and no response came back"** — suspect **framing** or **teardown**, in that order, before suspecting the peer. MCP over stdio is newline-delimited JSON (`LineFramer`, frames capped at 16 MiB); `ContentLengthFramer` is LSP-only and a `Content-Length` header from a peer now yields an explicit diagnostic. Teardown half-closes: child stdin closed, then responses drained until the server closes, bounded by a 10s grace, and `stderr` never triggers shutdown. **Both bugs are fixed**, so a fresh report of this shape means a regression in `pkg/protocol` or `pkg/proxy` — `git log -p` those two packages first. Route to `proxy-engine-developer`.
- **"A construction-only test suite was green while the feature was completely broken"** — not a test-quality nit, a triage signal. When a bug reaches a user through a path that has **no data-path test**, the regression test must exercise the real path (bytes in → bytes out, via `WithClientIO` for wrap), never the constructor. `wrap` shipped two fatal bugs behind a green suite that only ever called `NewWrapProxy`. If your fix's test doesn't send a message and read the answer, you haven't pinned the bug.
- **"The score changed for the same session"** — determinism or purity is broken. Suspect a clock, `math/rand` unseeded, map-iteration order in a decision, or a not-injected path that isn't a byte-exact no-op. Route to `chaos-gremlin-designer`.
- **"A dashboard type error appeared out of nowhere"** — the Go DTOs moved and `dashboard/lib/api/types.ts` didn't. That's the Go↔TS contract detector working, not a dashboard bug — and it's now fixable in the same commit.
- **"Passes on my machine"** — SQLite vs PostgreSQL divergence. The two repository implementations drifted on a dialect trap (booleans, timestamps, `ALTER`). Route to `database-engineer`.
- **A `-race` report in the wild** — never a flake. It's a correctness and, on a decision path, a security bug.

## Step 3 — Fix

One repo, one branch:

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
git checkout -b fix/<slug>
```

Route the fix by surface:
- `pkg/proxy`, `pkg/protocol` → `proxy-engine-developer`
- `internal/shield/{detection,policy,behavioral}` → `detection-pipeline-engineer`
- `internal/arena/{gremlins,scoring,session}` → `chaos-gremlin-designer`
- general Go (handlers, services, repos, CLI, config) → `go-backend-developer`
- `dashboard/` → `frontend-nextjs-developer`
- schema / query / `migrations/` → `database-engineer`

### Rules for the fix

- **Write the regression test first**, the one that would have caught this. A fix with no test invites the same bug back.
- **The test must exercise the path the bug travelled**, not the one that's easy to construct.
- **Minimal.** Only the reported defect. No scope creep, no drive-by refactor.
- **A `pkg/` fix is one commit, but its blast radius is the whole repo** — CLI, Shield and Arena all import it. Run the full gate, not just the package's tests.
- **A schema fix lands in both stores.**
- **A detection fix ships negative corpus cases.**
- Never weaken a test to make the fix pass.

## Step 4 — Verify

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
make check                          # vet + lint + go test -race
go test -race -count=2 ./...        # flake check
make dashboard-check                # typecheck + lint + vitest + build
```

Then **reproduce the original bug and confirm it's gone**, by the same path you used in Step 1. A green suite is not proof the reported symptom is fixed — in this repo that mistake has already shipped a product that never worked.

If the fix touched the proxy: `./bin/gremlyn wrap -- npx -y @modelcontextprotocol/server-memory`, and round-trip `initialize`, `tools/list`, `tools/call` — one JSON object per line, a matching response for each.

If the fix touched detection: run real traffic through and confirm nothing legitimate broke.

## Step 5 — Report

```markdown
## Bug: <title>

### Context
- Branch / HEAD: `<name>` · `<sha>`
- Tree clean at repro: ✅ / dirty (<files>)
- MCP servers behind the proxy: <names>

### Reproduction
<the exact steps / command that shows it>

### Root cause
<file:line + why it happens — the mechanism, not the symptom>

### Classification
<unclear | feature | duplicate | already-fixed | WAI | fixed>

### Fix
- Branch: `fix/<slug>`
- Changes: <file:line, one line each>
- Regression test: <test name — the one that now fails without the fix>
- Path it exercises: <the real data path, or say why a unit test is sufficient>

### Blast radius
- `pkg/` touched: yes/no → <what else imports it>
- Both stores updated: yes/no/n-a
- Dashboard types updated: yes/no/n-a

### Verification
- `make check`: ✅
- `-race -count=2`: ✅
- Original symptom reproduced then gone: ✅
- e2e against a real MCP server: ✅/n-a

### Follow-ups
- <missing `doctor` check, docs gap, adjacent bug found but not fixed>
```

## Rules

- **Check the tree and the binary before reading code.** Dirty working copy and stale `./bin` explain more confusing reports than logic errors.
- Reproduce before fixing; re-reproduce after.
- A regression test is part of the fix, not optional — and it must run the path the bug travelled.
- Never fix a false positive by loosening a pattern without negative corpus cases.
- A `-race` report is never dismissed as flaky.
- A `pkg/` fix is one commit and three consumers — verify the whole repo, never just the package.
- Report the adjacent bugs you found without fixing them. Don't silently expand scope.
