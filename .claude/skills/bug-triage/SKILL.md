---
name: bug-triage
description: Triage and fix bugs across the four Gremlyn repos — classify (unclear / feature / duplicate / works-as-intended / fixable), locate the root cause, fix on a branch with a regression test, and report. Use when the user says "triage bugs", "bug manager", "fix this bug", "work the backlog", or runs /bug-triage.
---

# Bug Triage

Triage and fix bugs in Gremlyn. Works from a local backlog file, a GitHub issue, or a bug the user describes in the conversation.

## Usage

```
/bug-triage                      # work the backlog
/bug-triage <issue-number>       # one GitHub issue
/bug-triage "<description>"      # a bug described inline
```

## Step 0 — Which repo, and which version

Before reading any code. This is the step that saves the most time in a four-repo project.

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
for d in gremlyn-core gremlyn-shield gremlyn-arena gremlyn-dashboard; do
  echo "=== $d  $(git -C $d log -1 --format='%h %s')"
done
grep -H 'gremlyn-core' gremlyn-shield/go.mod gremlyn-arena/go.mod
```

**Half the confusing bugs in this project are version skew, not logic errors.** A shield bug that appeared with no shield change is almost always a `pkg/` change. Check that before anything else.

If the report comes from a user, get `gremlyn version` output, their MCP client config, and which MCP servers sit behind the proxy. If you had to ask for any of it, that's a missing `gremlyn doctor` check — note it as a follow-up.

## Step 1 — Reproduce

A bug you can't reproduce is a bug you can't verify you fixed.

```bash
# is it in the proxy path?
cd gremlyn-core && go build -o gremlyn ./cmd/gremlyn
./gremlyn wrap -- npx @modelcontextprotocol/server-memory

# is it service-side?
cd gremlyn-shield && go run ./cmd/shield      # :8081
cd gremlyn-arena  && go run ./cmd/arena       # :8082

# is it dashboard-side?
cd gremlyn-dashboard && npm run dev           # :3000
```

Check the operational causes before the code ones — they're more common than logic bugs:
- an orphaned `shield`/`arena` process holding a port or the SQLite write lock (`pgrep -af 'shield|arena'`)
- a stale binary against a migrated DB
- `~/.gremlyn/` permissions, disk full
- an optional dependency down (Redis, ML sidecar) putting a layer on its degraded path

## Step 2 — Classify

Read the report, then trace the root cause in code (Grep/Read). Read the relevant `CLAUDE.md` and `docs/` first — for behavior questions, `.claude/agents/mcp-domain-expert.md` documents what the semantics are *supposed* to be, which often settles "bug or not" immediately.

| Branch | When | Action |
|--------|------|--------|
| **Version skew** | Consumer broke with no consumer change | Not a bug in the consumer. Report the core commit responsible. Fix belongs in core, or the consumer needs a bump |
| **Unclear / no repro** | Missing steps, version, or MCP server context; can't locate the defect | Ask for the SPECIFIC missing info. Stop |
| **Actually a feature** | Requests new behavior, not a defect | Say why it's a feature, route to `product-manager`. Stop |
| **Duplicate** | Same root cause as another open item | Reference it. Stop |
| **Already fixed** | Current code does the right thing | Say what fixed it, and add the regression test if it's missing |
| **Works as intended** | Code is correct; behavior is documented | Say what you checked, cite the doc. If the docs were unclear, that's a docs bug — fix it |
| **Clean fixable bug** | Root cause found, focused fix is clear | → Step 3 |

### Gremlyn-specific classification traps

- **"Shield blocked something legitimate"** — that's a **false positive**, the expensive failure mode. It's a real bug, and the fix is a detection change with negative corpus cases (`/new-detection-rule`), never a widened pattern applied by feel.
- **"The agent hung"** — check whether Shield produced a **silent drop instead of a JSON-RPC error**. A block the agent can't see becomes a hang, and that's a bug in the block path, not the agent.
- **"The score changed for the same session"** — determinism or purity is broken. Suspect a clock, `math/rand` unseeded, map-iteration order in a decision, or a not-injected path that isn't a byte-exact no-op. Route to `chaos-gremlin-designer`.
- **"A dashboard type error appeared out of nowhere"** — the Go DTOs moved and `lib/api/types.ts` didn't. That's the cross-repo contract detector working, not a dashboard bug.
- **"Passes on my machine"** — SQLite vs PostgreSQL divergence. The two repository implementations drifted on a dialect trap (booleans, timestamps, `ALTER`). Route to `database-engineer`.
- **A `-race` report in the wild** — never a flake. It's a correctness and, on a decision path, a security bug.

## Step 3 — Fix

Work on a branch in **the repo that owns the bug**:

```bash
cd <the-right-repo>
git checkout -b fix/<slug>
```

Route the fix by surface:
- `pkg/proxy`, `pkg/protocol` → `proxy-engine-developer`
- `shield/internal/{detection,policy,behavioral}` → `detection-pipeline-engineer`
- `arena/internal/{gremlins,scoring,session}` → `chaos-gremlin-designer`
- general Go (handlers, services, repos, CLI, config) → `go-backend-developer`
- dashboard → `frontend-nextjs-developer`
- schema / query → `database-engineer`

### Rules for the fix

- **Write the regression test first**, the one that would have caught this. A fix with no test invites the same bug back.
- **Minimal.** Only the reported defect. No scope creep, no drive-by refactor.
- **If the fix is in `pkg/`**, it's a two-part job: core fix + tagged version + consumer bumps. Say so; don't try to do it as one commit.
- **A schema fix lands in both stores.**
- **A detection fix ships negative corpus cases.**
- Never weaken a test to make the fix pass.

## Step 4 — Verify

```bash
# in the fixed repo
make check                          # vet + lint + go test -race
go test -race -count=2 ./...        # flake check
# dashboard
npm run typecheck && npm run lint && npx vitest run && npm run build
```

Then **reproduce the original bug and confirm it's gone**, by the same path you used in Step 1. A green suite is not proof the reported symptom is fixed.

If the fix touched the proxy: `./gremlyn wrap -- npx @modelcontextprotocol/server-memory` and exercise `initialize`/`tools/list`/`tools/call`.

If the fix touched detection: run real traffic through and confirm nothing legitimate broke.

## Step 5 — Report

```markdown
## Bug: <title>

### Repo & version
- Repo: <which>
- Core pinned: shield `<v>` · arena `<v>`
- Version skew ruled out: ✅

### Reproduction
<the exact steps / command that shows it>

### Root cause
<file:line + why it happens — the mechanism, not the symptom>

### Classification
<version-skew | unclear | feature | duplicate | already-fixed | WAI | fixed>

### Fix
- Branch: `fix/<slug>` in `<repo>`
- Changes: <file:line, one line each>
- Regression test: <test name — the one that now fails without the fix>

### Cross-repo impact
- Core `pkg/` touched: yes/no → <consumers needing a bump>
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

- **Check version skew before reading code.** It's the most common cause of a confusing report here.
- Reproduce before fixing; re-reproduce after.
- A regression test is part of the fix, not optional.
- Never fix a false positive by loosening a pattern without negative corpus cases.
- A `-race` report is never dismissed as flaky.
- Core `pkg/` fixes are always a multi-repo, multi-commit job — never pretend otherwise.
- Report the adjacent bugs you found without fixing them. Don't silently expand scope.
