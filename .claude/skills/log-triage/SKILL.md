---
name: log-triage
description: Triage zerolog error output from shield/arena, plus goroutine leaks and panics — classify noise vs real bugs, group by root cause, and fix. The local-first replacement for a hosted error tracker. Use when the user says "triage logs", "what are these errors", "check the logs", "shield is logging errors", or runs /log-triage.
---

# Log Triage

Gremlyn is **local-first** — there is no Sentry, no APM, no hosted error stream. The structured zerolog output from `shield` and `arena` is the entire error surface. This skill grooms it.

## Usage

```
/log-triage                    # triage recent output from a running stack
/log-triage <logfile>          # a captured log file
/log-triage panic              # focus on a panic / crash
/log-triage leak               # goroutine or memory leak hunt
```

## Step 1 — Capture

zerolog writes structured JSON. Capture it rather than reading it fly-by:

```bash
cd Shield && go run ./cmd/shield 2>&1 | tee /tmp/shield.log
cd Arena  && go run ./cmd/arena  2>&1 | tee /tmp/arena.log
```

Raise the level if the interesting lines aren't there (debug level is where the proxy detail lives).

Then group — **the top pattern by count is where the signal is**, not the most alarming single line:

```bash
jq -r 'select(.level=="error") | .message' /tmp/shield.log | sort | uniq -c | sort -rn | head -20
jq -r 'select(.level=="warn")  | .message' /tmp/shield.log | sort | uniq -c | sort -rn | head -20
jq -r 'select(.level=="error") | "\(.message)\t\(.error)"' /tmp/shield.log | sort | uniq -c | sort -rn | head -20
```

## Step 2 — Triage into three buckets

### 🔇 Noise — expected, but consider lowering the level
- An optional dependency down on a **documented degraded path** (Redis refused, ML sidecar unreachable) while the fallback engaged and the degradation event fired. Logged once at startup = fine. Logged per request = a level bug, downgrade it or rate-limit it.
- A malformed JSON-RPC message passed through as designed — this is the proxy behaving correctly against hostile input, and it should be `debug`/`info`, not `error`. An inline proxy that logs `error` for every malformed byte from a misbehaving MCP server drowns its own real signal.
- A client disconnecting mid-WebSocket-session — expected user behavior.
- `context.Canceled` on shutdown.

**Fix for noise is a level change, not silence.** Never delete the line.

### 🐛 Real bugs — fix
Priority order:
1. **Silent fail-open** — a dependency failed and traffic was allowed with no degradation event. The most serious thing this triage can find. Route to `security-reviewer` + `detection-pipeline-engineer` immediately.
2. **Panic / crash** — the proxy is inline with the user's agent; a panic takes their agent down. Always critical.
3. **A block that produced no event** — an unauditable security decision.
4. **Goroutine or memory growth** — on a long-running local process this ends as an OOM on the user's machine.
5. **Errors tied to real user impact** — a tool call that failed, a session that died.
6. **High-volume errors** — even if individually harmless, they're hiding everything else.

### 🚨 Immediate escalation, regardless of count
- **A payload in a log line.** Log identifiers, never content. A tool call body in a log is a PII/credential leak, and it's the same class of bug whether it appears once or a million times. → `security-reviewer`
- **A `-race` report.** Never a flake in this codebase; on a policy decision path it's a vulnerability.

## Step 3 — Known patterns and their fixes

| Log signature | Root cause | Fix |
|---|---|---|
| `context deadline exceeded` in a detection layer | L2/L3 timeout on a slow dependency | Confirm the documented fallback engaged AND emitted a degradation event. If it silently allowed, that's a fail-open |
| `connection refused` to the ML sidecar or Redis, repeated per request | The fallback works but logs at `error` every time | Log once + a counter/metric, not per request |
| `parse jsonrpc: unexpected end of JSON input` | A malformed message from an MCP server | Correct behavior. Should be `debug`. If it kills the connection instead of passing through, that's a real bug → `proxy-engine-developer` |
| `database is locked` (SQLite) | Another writer — an orphaned process, or an open `sqlite3` shell | `pgrep -af 'shield\|arena'`. Confirm WAL is on. If it's genuine write contention, that's SQLite's single-writer design → `database-engineer` |
| `sql: database is closed` | Repo used after shutdown, or a goroutine outliving its ctx | A missing ctx cancel path → the owning dev agent |
| `no such column` / `no such table` | Migration divergence between SQLite and PostgreSQL, or a stale binary against a migrated DB | → `database-engineer` |
| `panic: send on closed channel` | WebSocket broadcast racing session teardown | → `chaos-gremlin-designer` (session lifecycle) |
| `panic` inside a pipeline stage | A stage panicking out into the proxy — the recover boundary is missing or wrong | 🔴 `proxy-engine-developer` |
| Goroutine count climbing | A leak: blocked channel send, or a goroutine not exiting on ctx cancel | See §4 |
| `redact` applied but the payload still contains the secret | Redaction computed on a normalized copy instead of the forwarded bytes | 🔴 `security-reviewer` + `detection-pipeline-engineer` |
| Slow-query warnings on `events` | Missing composite index or `OFFSET` pagination | → `database-engineer`, or `/optimize-endpoint` |

## Step 4 — Leak hunting

```bash
# reproduce: repeated connect/disconnect, or repeated sessions
# then, if a pprof endpoint is exposed:
go tool pprof -top http://localhost:8081/debug/pprof/goroutine
go tool pprof -top http://localhost:8081/debug/pprof/heap
```

Goroutine count over time must be **flat**. A rising count under a repeated connect/disconnect cycle is a leak, not warm-up.

Where they live in this codebase:
- proxy full-duplex goroutines not exiting on ctx cancel → `proxy-engine-developer`
- a WebSocket subscriber never unregistered → `frontend-nextjs-developer` (client side) or the arena ws handler
- a session goroutine surviving a cancelled session → `chaos-gremlin-designer`

Reproduce it in a test with `runtime.NumGoroutine()` before/after — that's the regression test.

## Step 5 — Present the triage, then ask

**Human-in-the-loop checkpoint.** Before changing anything:

| Pattern | Count | Bucket | Root cause | Proposed action |
|---|---|---|---|---|
| `connection refused` (redis) | 1,847 | Noise | fallback works, logs per request | Downgrade to a startup warning + counter |
| `panic: send on closed channel` | 3 | 🔴 Bug | ws broadcast races teardown | Fix session teardown ordering |
| `redact` leaked span | 1 | 🚨 | spans on normalized copy | Escalate to security-reviewer |

Then ask: **"Does this triage look right? Anything you want handled differently?"**

Wait for confirmation before fixing.

## Step 6 — Fix and verify

- Group by **root cause**, not by log line. Twenty error lines from one missing ctx cancel are one fix.
- One fix per commit, in the repo that owns it. Conventional Commits.
- **Every fix ships the test that would have caught it.**
- A level change is a legitimate fix — but state that you changed the level and didn't change behavior.

```bash
make check
go test -race -count=2 ./...
```

Then re-run the stack and confirm the pattern is gone from the log, not just from the tests.

## Report Format

```markdown
## Log Triage — <date>

### Volume
- errors: <N> across <M> distinct patterns
- warns: <N> across <M>

### 🚨 Escalated
- <payload in logs / race / fail-open> → <agent>

### 🐛 Fixed
| pattern | count | root cause | fix | test |
|---|---|---|---|---|

### 🔇 Level-adjusted (behavior unchanged)
| pattern | old level | new level | why |
|---|---|---|---|

### Left alone
- <pattern> — <why it's expected>

### Follow-ups
- <a `doctor` check that would have surfaced this earlier>
```

## Rules

- **Group by root cause, never by log line.**
- The top pattern by count matters more than the scariest single line.
- **Noise gets a level change, never deletion.**
- A payload in a log is an escalation regardless of frequency.
- A `-race` report is never a flake.
- A silent fail-open outranks everything else in this triage.
- Every fix ships a regression test.
- Ask before fixing.
