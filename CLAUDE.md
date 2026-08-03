# Gremlyn — Chaos engineering & security for AI agents

Gremlyn sits between an AI agent and its MCP (Model Context Protocol) servers.

- **Arena** — chaos testing. Injects controlled failures ("gremlins") into the agent's MCP pipeline and scores its resilience. *The flagship.*
- **Shield** — MCP firewall. Applies security policies, prompt-injection detection, PII redaction, behavioral monitoring.
- Both plug into the shared **proxy engine** in `pkg/proxy` via the pipeline hook system.

**Local-first**: runs on the user's machine, SQLite by default, nothing leaves the box unless explicitly enabled.

## Repo layout — single Go module

```
cmd/gremlyn/          → CLI entry point (cobra)
cmd/shield/           → Shield service (:8081)
cmd/arena/            → Arena service (:8082)
pkg/                  → shared, importable
  protocol/           → MCP message types, transport abstraction
  proxy/              → proxy engine: wrap, httpproxy, jsonrpc, pipeline
  config/             → gremlyn.yaml, MCP client config detection
  models/             → shared domain models
  datadir/            → ~/.gremlyn resolution
internal/cli/         → CLI commands
internal/shield/      → api, policy, detection, alert, service, storage
internal/arena/       → api, gremlins, scoring, session, service, storage
migrations/{shield,arena}/  → PostgreSQL migrations (SQLite auto-migrates)
dashboard/            → Next.js 15 frontend (:3000)
docs/                 → per-area reference: core.md, shield.md, arena.md, dashboard.md
```

Was four separate repos until the 2026-08-04 merge. One module, one version, one commit per change.

## Tech stack

**Go 1.26** · cobra (CLI) · chi (HTTP) · gorilla/websocket · zerolog · pgx/v5 · `modernc.org/sqlite` (pure Go) · testify

**Dashboard**: Next.js 15 App Router · React 19 · TypeScript strict · Tailwind 4 · Zustand · Chart.js · Vitest

## Commands

```bash
make build            # all three binaries into bin/
make check            # THE gate: vet + lint + test -race
make test             # go test ./... -race
make fmt
make dashboard-check  # typecheck + lint + vitest + build

# run the stack
./bin/shield          # :8081
./bin/arena           # :8082
cd dashboard && npm run dev   # :3000

# the product, end to end
./bin/gremlyn wrap -- npx @modelcontextprotocol/server-memory
./bin/gremlyn doctor
```

Data lives in `~/.gremlyn/{shield,arena}.db`. Deleting those files resets local state.

## Code style — non-negotiables

- **Strong typing.** Real structs, never `map[string]interface{}` for a known shape. Use `json.RawMessage` to defer a decode deliberately.
- **Errors always wrapped**: `fmt.Errorf("create session: %w", err)`. Never discarded. Domain errors are sentinels compared with `errors.Is`.
- **Godoc on every exported symbol** (enforced by `revive`).
- **`ctx context.Context` first** on every IO function. No `context.TODO()` shipped.
- **No global mutable state, no init-time side effects** — Arena instantiates a proxy per session.
- **Interfaces defined in the consumer**, 1–3 methods. Accept interfaces, return structs.
- **Raw SQL, parameterized always** (`?` SQLite / `$1` pgx). No ORM. No string concatenation into a query.
- **Thin handlers**: handler → service → repo. No business logic in HTTP handlers.
- **zerolog only.** No `fmt.Println`, no `log.Printf`. Log identifiers, **never payloads** (they contain PII and credentials).
- **No CGO.** `CGO_ENABLED=0` must keep working — it's why storage uses pure-Go SQLite and how static binaries ship.

## Load-bearing invariants

These aren't style preferences — breaking them silently produces numbers nobody can trust:

- **`internal/arena/scoring` is pure.** `[]ArenaEvent → ResilienceReport`, no clock, no randomness, no IO, no map-iteration order in a decision. This is what makes two reports comparable.
- **Gremlins are deterministic under a seed**, and `injected == false` is a **byte-exact no-op**. This is what makes a session replayable and control runs uncontaminated.
- **Only one gremlin injects per message.** Compounding mutations make outcome attribution impossible.
- **The dual store must behave identically.** A schema change lands in SQLite *and* PostgreSQL. Never edit a shipped embedded SQLite migration — append.
- **A Shield `block` is never downgraded** by a later detection layer, and it must be observable to the agent (a JSON-RPC error, not a silent drop — a silent drop becomes a hang).
- **The pipeline is the only extension point.** Adding a rule or a gremlin must never require a `pkg/proxy` change.

## Testing

`make check` is the gate. `-race` is **mandatory** — the proxy is full-duplex, Arena runs concurrent sessions, Shield decides under load. **A `-race` report is never a flake**; on a decision path it's a vulnerability.

Table-driven tests, `_test.go` next to the code, `testify`. `scenarios_test.go` for cross-component behavior within a package. Integration tests behind `//go:build integration`.

`go test ./...` must pass on a clean machine with **no Docker, no PostgreSQL, no Redis** — zero-config is a product feature.

Detection changes ship **negative** corpus cases, not just positives. False positives break the user's agent, which is the expensive failure mode.

## Git

Branches `feat/xxx`, `fix/xxx`, `refactor/xxx`. Conventional Commits. `make check` before every commit.

## Where to look next

| Area | Doc |
|---|---|
| Proxy engine, protocol, CLI | `docs/core.md` |
| Firewall, detection, policy | `docs/shield.md` |
| Gremlins, scoring, sessions | `docs/arena.md` |
| Frontend, GREMLYN_OS design system | `docs/dashboard.md` |
| Roadmap & current priorities | `PLAN.md` |
| Conventions, agents, skills | `.claude/` |

⚠️ `dashboard/reference/{arena,dashboard}.html` is the **read-only** source of truth for every visual decision.
