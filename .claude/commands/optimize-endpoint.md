# Optimize Endpoint — Perf-Safe Go Endpoint & Hot-Path Optimization

Optimize a slow Gremlyn endpoint or hot path while guaranteeing **byte-identical responses and identical observable behavior**. Methodology: measure → profile → fix → verify integrity → re-measure.

## Input

User supplies ONE of:
- An API path: `GET /api/v1/events?limit=50`
- A handler or service name: `ListEvents`, `EventStore.List`
- A hot path package: `pkg/proxy`, `internal/detection`
- A dashboard page whose data feels slow: `/shield/events`

## ⚠️ Prerequisite: realistic data volume

**A local DB with 200 rows hides every problem this command exists to find.** Before measuring anything, confirm the volume:

```bash
sqlite3 ~/.gremlyn/shield.db "SELECT COUNT(*) FROM events;"
sqlite3 ~/.gremlyn/arena.db  "SELECT COUNT(*) FROM arena_events;"
```

If it's small, generate volume first (a loop inserting representative rows, or a long `gremlyn wrap` session against a real MCP server). Aim for 100k–1M rows on the event tables. Say what volume you measured at — a number without its dataset size is meaningless.

## Workflow

### 1. Locate the code

- API path → `Grep` the chi router: `internal/<product>/api/router.go`, then the handler in `internal/<product>/api/handlers.go`, then the service, then the repository method.
- Note which store is in play: **SQLite by default**, PostgreSQL if `DATABASE_URL` is set. Optimize the one that's actually slow, and check whether the fix applies to both.
- Hot path → `pkg/proxy` (per-message) or `internal/detection` (per-payload).

### 2. Baseline

**API endpoint** — measure through the router, not the handler in isolation:

```bash
go run ./cmd/shield &          # :8081
for i in $(seq 5); do
  curl -s -o /dev/null -w "%{time_total}s  %{size_download}B\n" \
    -H "X-API-Key: $KEY" "localhost:8081/api/v1/events?limit=50"
done
```

**Query count** — the N+1 detector. Wrap the repo call in a test and count, or enable query logging. Then get the plan:

```bash
sqlite3 ~/.gremlyn/shield.db "EXPLAIN QUERY PLAN
  SELECT * FROM events WHERE server_id = ? ORDER BY created_at DESC LIMIT 50;"
# PostgreSQL:
psql "$DATABASE_URL" -c "EXPLAIN (ANALYZE, BUFFERS) <query>;"
```

**Hot path** — benchmark, don't time:

```bash
go test -bench=. -benchmem -count=10 ./pkg/proxy/ > before.txt
go test -bench=. -cpuprofile=cpu.out -memprofile=mem.out ./pkg/proxy/
go tool pprof -top cpu.out
go build -gcflags='-m' ./pkg/proxy/ 2>&1 | grep 'escapes to heap'
```

Record: **`BEFORE = ms (p50/p95), query_count, bytes`** for endpoints, **`ns/op, B/op, allocs/op`** for hot paths.

### 3. Identify the pattern

The recurring offenders in this codebase, in order of frequency:

| Pattern | Symptom | Fix |
|---|---|---|
| **N+1** | one query per row to fetch a rule/server name | Batch: collect ids, one `WHERE id IN (…)`, map in Go |
| **`OFFSET` pagination** | degrades linearly as the table grows | Keyset: `WHERE created_at < $cursor ORDER BY created_at DESC LIMIT n` |
| **Missing composite index** | `SCAN TABLE events` in the plan | `(server_id, created_at DESC)` — equality column first, range last |
| **`SELECT *` on an event table** | large `bytes`, slow even with an index | Select the columns the DTO actually uses |
| **Unbounded scan** | no time bound on an ever-growing table | Always bound the window |
| **Per-message full unmarshal** | high `allocs/op` in `pkg/proxy` | Peek `method`/`id`; `json.RawMessage` for payloads no stage touches |
| **Regex compiled per call** | `internal/detection` hot | Precompile once at construction |
| **Per-event INSERT** | slow chaos session | Batch inserts in the recorder, bounded, flush on terminal state |
| **Marshal per WebSocket subscriber** | scales with subscribers × events | Marshal once, write N times |

### 4. Apply ONE change, then re-measure

One at a time. Run the package tests before and after each. Then re-measure with the same command and the same dataset.

```bash
go test ./internal/<product>/api/ ./internal/<product>/service/ -v
go test -race -count=2 ./...
go test -bench=. -benchmem -count=10 ./pkg/proxy/ > after.txt
benchstat before.txt after.txt      # significance, not eyeballing
```

### 5. Verify integrity — the non-negotiable part

An optimization that changes the response is a bug, not a win.

```bash
# byte-identical response
curl -s -H "X-API-Key: $KEY" "localhost:8081/api/v1/events?limit=50" > before.json
# … apply change, restart …
curl -s -H "X-API-Key: $KEY" "localhost:8081/api/v1/events?limit=50" > after.json
diff <(jq -S . before.json) <(jq -S . after.json) && echo "IDENTICAL"
```

Also verify, explicitly:
- **Error values and wrapping unchanged** (`errors.Is` still matches)
- **Events still emitted**, same set, same order
- **Both stores** still behave identically if you touched a repository
- **Determinism** — if you touched arena, a seeded session still produces identical injections, and `scorer.go` is still pure
- **`-race -count=2` clean**

### 6. Hand off what's out of scope

- Needs a **new index or column** → stop, that's `database-engineer`. Never add a migration from this command.
- Needs to change **what a detector matches** or **what a gremlin injects** → stop, that's semantics: `detection-pipeline-engineer` / `chaos-gremlin-designer`.
- Needs a **core `pkg/` API change** → stop, that's a ticket split (`product-manager`).

## Output

```markdown
## Optimization: <target>

### Dataset
- events: <N> rows · store: sqlite | postgres

### Baseline
- p50 / p95: <ms> · queries: <N> · bytes: <N>
- bench: <ns/op, B/op, allocs/op>
- profile top: <hot symbol>
- plan: <SCAN TABLE … / USING INDEX …>

### Changes
1. <one-liner> — file:line

### After
- p50 / p95: <ms (-X%)> · queries: <N (-X)> · bytes: <N>
- bench: <ns/op (-X%), allocs/op (-N)>  · benchstat: <significant?>
- plan: <…>

### Integrity verified
- Response byte-identical: ✅ (jq -S diff)
- Error values / wrapping: unchanged
- Events emitted: same set, same order
- Both stores identical: ✅ / n/a
- Determinism (arena): ✅ / n/a
- `-race -count=2`: clean

### Handed off
- <index / semantics / core API work, and to whom>
```

## Rules

- **Never change observable behavior.** Byte-identical responses, or it's not an optimization.
- One change at a time, measured each time.
- **No numbers = no claim.** `benchstat` for significance.
- No schema changes from this command.
- Never weaken or skip a test to make it pass.
- Always state the dataset size a number was measured at.

## Start

Optimize: $ARGUMENTS
