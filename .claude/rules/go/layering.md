---
paths:
  - "**/*.go"
  - "**/go.mod"
---

# Go Layering Rules — Gremlyn

Always-on, enforceable contract for Go code across the three Go repos. Per-repo detail:
`docs/core.md`, `docs/shield.md`, `docs/arena.md`.
When this file and a repo's CLAUDE.md disagree, the CLAUDE.md wins — this is the short "always / never".

We organize each service as **layered packages under `internal/`**, with a shared library in `pkg/`. Two ideas carry everything: **dependencies point inward toward the domain**, and **the pipeline is the only extension point**.

---

## 1. Repo skeleton

### `gremlyn-core` — library first
```
pkg/                  # PUBLIC — importable by shield and arena
  protocol/           # MCP message types, transport abstraction
  proxy/              # proxy engine: wrap, httpproxy, jsonrpc, pipeline
  config/             # gremlyn.yaml parsing, MCP client config detection
  models/             # shared domain models
  datadir/            # ~/.gremlyn resolution
internal/cli/         # CLI-ONLY. Never importable, never needed by a service.
cmd/gremlyn/          # cobra entry point
```

### `gremlyn-shield` / `gremlyn-arena` — services
```
cmd/<svc>/main.go            # wiring only: read config, construct, serve
internal/service/            # orchestration — the top of the domain
internal/api/                # chi router, handlers, middleware, DTOs
internal/<domain>/           # the actual logic: policy/ detection/ behavioral/ alert/
                             #                   gremlins/ session/ scoring/
internal/storage/sqlite/     # repositories (default store)
internal/storage/postgres/   # repositories (optional store)
migrations/                  # PostgreSQL golang-migrate files
```

---

## 2. The dependency rule — imports point toward the domain

- **`cmd/`** → `internal/service`, `internal/api`, `internal/storage`. Wiring only. Zero logic.
- **`internal/api`** → `internal/service`. **Thin**: parse → call service → write response. Never imports a repository, never imports a domain package directly.
- **`internal/service`** → domain packages + repository **interfaces**. Never imports `internal/api`, never touches `http.Request`/`http.ResponseWriter`.
- **`internal/<domain>`** (policy, detection, gremlins, scoring, …) → `core/pkg` + stdlib. **Never imports `internal/api`, `internal/storage`, or `internal/service`.**
- **`internal/storage/*`** → domain types + the interfaces defined by `service`. Never imports `service` or `api`.
- **`core/pkg/*`** → stdlib + narrow third-party. **Never imports `core/internal`.**
- **`core/internal/cli`** → `core/pkg`. If a service ever needs something from here, that thing belongs in `pkg/`.

**No business logic in handlers.** **No IO in domain packages** — a scorer that reads a file, or a gremlin that logs to a DB, has broken the layering.

## 3. Interfaces belong to the consumer

The service defines what it needs; storage implements it.

```go
// internal/service/shield.go — the CONSUMER owns the interface
type EventStore interface {
    Insert(ctx context.Context, e models.Event) error
    List(ctx context.Context, f EventFilter) ([]models.Event, error)
}
```

- Keep interfaces **1–3 methods**. A 12-method `Store` interface is a package boundary that failed.
- **Accept interfaces, return structs.**
- Never define an interface in the storage package "so both stores can implement it" — that inverts the dependency and couples the service to storage's vocabulary.

## 4. The dual-store rule

`storage/sqlite/` and `storage/postgres/` implement the **same service-owned interfaces** and must behave **identically**.

- Add a method to one → add it to the other. The build breaking is the point.
- A schema change lands in **both**: append to `internal/storage/sqlite/migrations.go` (embedded, auto-applied) **and** add `migrations/NNN_*.{up,down}.sql`.
- **Never edit a shipped embedded SQLite migration.** A user's DB already ran it. Append a new one.
- Dialect differences (`?` vs `$1`, booleans, timestamps, JSON functions) are **normalized inside the repository**. They must never leak into `service` or a domain package.
- SQLite is the **default** and needs no external service. That zero-config path must always work — see `.claude/rules/go/testing-convention.md` §4.

## 5. The pipeline is the only extension point

`core/pkg/proxy/pipeline.go` is the seam between the shared engine and the two products.

- Shield hooks **decide** (return an action). Arena gremlins **mutate** (return a modified message). Both are pipeline stages.
- **Neither service may require a core code change to add a rule or a gremlin.** If it does, the seam is wrong — fix the seam, not the caller.
- Stage ordering is part of the contract. Changing it is a documented breaking change.
- A stage must not panic out into the proxy; a stage error must not silently skip the remaining stages.

## 6. Cross-repo discipline

Shield and arena import `github.com/gremlyn-ai/gremlyn/pkg/...`; local dev uses `replace … => ../gremlyn-core`.

- **Shared types live in `core/pkg` FIRST**, then get imported. Never duplicate a type across shield and arena — divergent copies of an `Event` is the failure mode this rule prevents.
- A `pkg/` signature change is **breaking for two consumers**. Core changes ship in their own commit, with both consumers verified to build.
- Version order is fixed: tag core → `go get` in each consumer → build → tag consumers. Never the reverse.

## 7. Strong typing, no exceptions

- Real structs for known shapes. **`map[string]interface{}` is banned** for anything with a known schema; use `json.RawMessage` when you're deliberately deferring a decode.
- Godoc on every exported symbol (enforced by `revive`).
- `json` tags on every struct field; `yaml` tags on config.
- Every error wrapped: `fmt.Errorf("create session: %w", err)`. Domain errors are sentinels/custom types compared with `errors.Is`.
- `ctx context.Context` first on every IO function.
- **No global mutable state, no init-time side effects.** Arena instantiates a proxy per session — package-level state makes concurrent sessions leak into each other.
- Dependency injection via constructors; functional options past ~3 optional knobs.
- Short receivers: `p *Proxy`, `s *Service`, `c *Config`.

## 8. Purity where it's load-bearing

Two places where purity is a correctness requirement, not a style preference:

- **`arena/internal/scoring`** — `scorer.go` is a pure function `[]ArenaEvent → ResilienceReport`. No clock, no randomness, no IO, no map-iteration order in a decision. This is what makes two reports comparable.
- **Gremlin injection** — deterministic under a seed, and `injected == false` is a byte-exact no-op. This is what makes a session replayable.

Breaking either produces numbers nobody can trust, silently.

## 9. Testing

Test domain packages directly. Mock IO at the **interface the consumer defined**, not at the concrete repo. Test handlers through the router with `httptest`. Test both stores against the same interface test. Full bar: `.claude/rules/go/testing-convention.md`.
