---
paths:
  - "**/*_test.go"
  - "**/testdata/**"
---

# Testing Rules — Go

Pairs with `.claude/rules/go/layering.md` (§9) and each repo's CLAUDE.md "Testing rules".

> **The rule:** every package has a `_test.go`. Every layer is covered before the work is considered done. `-race` is part of the gate, not an extra.

---

## 1. Where tests live

Next to the code, never in a parallel tree:

```
internal/shield/policy/engine.go        → engine_test.go
internal/shield/policy/matcher.go       → matcher_test.go
internal/shield/policy/scenarios_test.go   # cross-component behavior for the package
pkg/proxy/jsonrpc.go             → jsonrpc_test.go
```

- `<file>_test.go` for the unit tests of `<file>.go`.
- `scenarios_test.go` for cross-component behavior within a package — this pattern already exists in `shield/internal/{api,policy}`, `arena/internal/{gremlins,scoring,session}`. **Extend those rather than inventing a new file layout.**
- `_integration_test.go` with `//go:build integration` for anything needing an external service (PostgreSQL, Redis, the ML sidecar).
- Fixtures in `testdata/` (already used in `testdata/`).

## 2. Conventions

- **Table-driven** for anything with more than one input case. Named subtests via `t.Run(tt.name, …)` so a failure names itself.
- `testify/require` for fatal preconditions, `testify/assert` for the assertions themselves. Don't `assert` a nil-check and then dereference.
- Mock at the **interface the consumer defined** (`service.EventStore`), never the concrete repository type.
- `t.Cleanup` over `defer` for teardown; `t.TempDir()` for scratch files.
- `context.Background()` in tests; `context.WithTimeout` when asserting cancellation.
- **Never** hit a real LLM, a real remote MCP server, or the network in a unit test.
- Assert **observable behavior**, not unexported internals. A test that reads a private field breaks on every refactor and proves nothing.
- No `time.Sleep` to synchronize. Use channels, `sync.WaitGroup`, or `require.Eventually`.

## 3. What each layer must cover

- **`pkg/proxy`, `pkg/protocol` — the highest bar in the codebase.** Every message that crosses the proxy comes from an untrusted source, so the parser table must include: valid request / notification / response, batch, missing `id`, wrong `jsonrpc` version, truncated JSON, oversized payload, non-UTF8 bytes, empty message. Assert **envelope invariants**: `id` preserved on responses, no panic, no silent drop. Pipeline tests use fake stages: no-op, mutating, short-circuiting, erroring, panicking (must be contained). Both transports (`wrap`, `httpproxy`) covered. A benchmark accompanies any change to per-message work.

- **`internal/detection` — corpus tests, positives AND negatives.** A new pattern with no new negative cases is incomplete work: false positives break the user's agent, which is the expensive failure mode here. Include the **meta-corpus** class explicitly — security docs and this repo's own tests legitimately contain injection strings and must not match. Also cover every dependency being down (sidecar 500, LLM timeout, Redis refused) and assert the **documented fallback**, not merely "no panic".

- **`internal/policy` — every action type** (block, redact, alert, throttle, allow), precedence between them, and the tie-breaking rule. Matcher specificity ordering tested explicitly — that's where subtle security bugs live. Rate limit tested at the boundary (last allowed, first rejected) and for key isolation across tools.

- **`internal/gremlins`** — per gremlin: injected path, **not-injected path asserted byte-exact unchanged**, each message kind it may see, a kind it must ignore, `ctx` cancelled mid-inject, the boundary of every bound (max size / delay / iterations), and a **determinism test** (same seed + same input → identical decisions, run twice). Plus an envelope test: `id`/`jsonrpc`/`method` survive unless the gremlin declares an exception.

- **`internal/scoring`** — all survived, all crashed, **empty session**, single event, gremlin enabled but never fired, mixed. Plus **monotonicity** (a strictly more resilient event stream scores strictly higher) and **purity** (same input → same report twice, and the input slice is not mutated). No edge case may yield `NaN` or divide by zero.

- **`internal/session`** — every legal transition (`Created → Running → Completed`, `→ Cancelled`) and rejection of every illegal one. Cancel mid-run retains prior events and yields a report flagged partial. Two concurrent sessions don't leak state into each other (under `-race`).

- **`internal/api` — functional, through the router.** `httptest.NewRecorder` + the real chi router, not the handler function in isolation. Cover: success shape, validation-error shape (`{"error":…,"code":…}`), query params, and **auth on every endpoint** — valid key 200, invalid key 401, missing key 401. No endpoint is exempt without a written reason. WebSocket handlers use `gorilla/websocket` test helpers: upgrade, message routing, unknown `type` ignored, client disconnect mid-session (session continues, events persist), backpressure.

- **`internal/storage`** — write the interface test **once** and run it against **both** SQLite and PostgreSQL. A repository method tested only on SQLite leaves the PG twin unverified, and the PG twin is where a concatenated query hides.

## 4. Zero-config is a test requirement

SQLite is the default store and needs nothing installed. **The default test run must work with no Docker, no PostgreSQL, no Redis, no sidecar:**

```bash
go test ./...            # must pass on a clean machine
```

Anything needing an external service is `//go:build integration` and runs separately:

```bash
docker compose up -d postgres redis
go test -tags=integration ./...
```

A unit test that silently requires a running service breaks the zero-config promise and will fail for anyone but you.

## 5. `-race` and flake policy

```bash
go test -race ./...          # part of `make check` — the gate
go test -race -count=3 ./... # flake hunting
```

- **`-race` is mandatory.** The proxy is full-duplex, Arena runs concurrent sessions, Shield decides under load. A non-race run proves very little here.
- **A `-race` report is never dismissed as flaky.** A race on a policy decision can yield an `allow` where a `block` was computed — that's a vulnerability. Fix it, and involve `security-reviewer` if it's on a decision path.
- **Flake = bug.** Mark it, route it, never retry-loop it away.
- Never weaken, skip, or delete a test to make a change pass. That is a behavior change in disguise.

## 6. Coverage

```bash
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
make coverage    # HTML report
```

Coverage is a diagnostic, not a target. But three packages should be near-exhaustive because a gap there is a shipped defect: **`pkg/proxy`**, **`internal/policy`**, **`internal/detection`**. Don't chase a percentage on `cmd/` wiring.
