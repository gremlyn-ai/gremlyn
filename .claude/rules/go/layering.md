---
paths:
  - "**/*.go"
  - "**/go.mod"
---

# Go Layering Rules — Gremlyn

Always-on, enforceable contract for Go code in the single module `github.com/gremlyn-ai/gremlyn`.
Per-area detail: `docs/core.md`, `docs/shield.md`, `docs/arena.md`.
When this file and the root `CLAUDE.md` disagree, `CLAUDE.md` wins — this is the short "always / never".

We organize each product as **layered packages under `internal/<product>/`**, with the shared engine in `pkg/`. Two ideas carry everything: **dependencies point inward toward the domain**, and **the pipeline is the only extension point**.

---

## 1. Module layout

```
cmd/gremlyn/          # cobra entry point — the CLI
cmd/shield/           # Shield service (:8081) — wiring only
cmd/arena/            # Arena service (:8082) — wiring only
pkg/                  # PUBLIC — the shared surface, importable from anywhere
  protocol/           # MCP message types, transport abstraction, line framing
  proxy/              # proxy engine: wrap, httpproxy, jsonrpc, pipeline
  config/             # gremlyn.yaml parsing, MCP client config detection
  models/             # shared domain models
  datadir/            # ~/.gremlyn resolution
internal/cli/         # CLI-ONLY. Never imported by a service.
internal/shield/      # the firewall product
internal/arena/       # the chaos product
migrations/{shield,arena}/   # PostgreSQL golang-migrate files
```

Each product under `internal/` has the same layered shape (`<product>` = `shield` | `arena`):
```
internal/<product>/service/            # orchestration — the top of the domain
internal/<product>/api/                # chi router, handlers, middleware, DTOs
internal/<product>/<domain>/           # the actual logic: policy/ detection/ alert/
                                       #                   gremlins/ session/ scoring/
internal/<product>/storage/sqlite/     # repositories (default store)
internal/<product>/storage/postgres/   # repositories (optional store)
```

---

## 2. The dependency rule — imports point toward the domain

- **`cmd/<svc>`** → `internal/<product>/{service,api,storage}`. Wiring only. Zero logic.
- **`internal/<product>/api`** → `internal/<product>/service`. **Thin**: parse → call service → write response. Never imports a repository, never imports a domain package directly.
- **`internal/<product>/service`** → domain packages + repository **interfaces**. Never imports `api`, never touches `http.Request`/`http.ResponseWriter`.
- **`internal/<product>/<domain>`** (policy, detection, gremlins, scoring, …) → `pkg/` + stdlib. **Never imports `api`, `storage`, or `service`.**
- **`internal/<product>/storage/*`** → domain types + the interfaces defined by `service`. Never imports `service` or `api`.
- **`pkg/*`** → stdlib + narrow third-party. **Never imports `internal/`** — see §6.
- **`internal/cli`** → `pkg/`. If a service ever needs something from here, that thing belongs in `pkg/`.

**No business logic in handlers.** **No IO in domain packages** — a scorer that reads a file, or a gremlin that logs to a DB, has broken the layering.

## 3. Interfaces belong to the consumer

The service defines what it needs; storage implements it.

```go
// internal/<product>/service/shield.go — the CONSUMER owns the interface
type EventStore interface {
    Insert(ctx context.Context, e models.Event) error
    List(ctx context.Context, f EventFilter) ([]models.Event, error)
}
```

- Keep interfaces **1–3 methods**. A 12-method `Store` interface is a package boundary that failed.
- **Accept interfaces, return structs.**
- Never define an interface in the storage package "so both stores can implement it" — that inverts the dependency and couples the service to storage's vocabulary.

## 4. The dual-store rule

`internal/<product>/storage/sqlite/` and `.../storage/postgres/` implement the **same service-owned interfaces** and must behave **identically**.

- Add a method to one → add it to the other. The build breaking is the point.
- A schema change lands in **both**: add `internal/<product>/storage/sqlite/migrations/NNN_*.up.sql` and register it in `migrations.go` (embedded, auto-applied) **and** add `migrations/<product>/NNN_*.{up,down}.sql`.
- **Never edit a shipped embedded SQLite migration.** A user's DB already ran it. Append a new one.
- Dialect differences (`?` vs `$1`, booleans, timestamps, JSON functions) are **normalized inside the repository**. They must never leak into `service` or a domain package.
- SQLite is the **default** and needs no external service. That zero-config path must always work — see `.claude/rules/go/testing-convention.md` §4.

## 5. The pipeline is the only extension point

`pkg/proxy/pipeline.go` is the seam between the shared engine and the two products.

- Shield hooks **decide** (return an action). Arena gremlins **mutate** (return a modified message). Both are pipeline stages.
- **Neither product may require a `pkg/proxy` change to add a rule or a gremlin.** If it does, the seam is wrong — fix the seam, not the caller.
- Stage ordering is part of the contract. Changing it is a documented breaking change.
- A stage must not panic out into the proxy; a stage error must not silently skip the remaining stages.

## 6. In-module discipline

`internal/shield` and `internal/arena` both import `github.com/gremlyn-ai/gremlyn/pkg/...`. Same module, so the compiler is the enforcement mechanism — use it.

- **Shared types live in `pkg/` FIRST**, then get imported. Never duplicate a type between `internal/shield` and `internal/arena` — divergent copies of an `Event` is the failure mode this rule prevents. If both need it, it moves to `pkg/models`.
- **`pkg/` must never import `internal/`.** `pkg/` is the public, importable surface; an `internal/` import there is both a layering inversion and a wall for any external consumer.
- **`internal/cli` is CLI-only.** No service imports it, ever. It may import `pkg/`; the reverse is a bug.
- A `pkg/` change and its `internal/shield` + `internal/arena` updates are **one atomic commit**. `make check` builds and tests all of them in one run, so there is nothing to bump and no reason to split.

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

- **`internal/arena/scoring`** — `scorer.go` is a pure function `[]ArenaEvent → ResilienceReport`. No clock, no randomness, no IO, no map-iteration order in a decision. This is what makes two reports comparable.
- **Gremlin injection** — deterministic under a seed, and `injected == false` is a byte-exact no-op. This is what makes a session replayable.

Breaking either produces numbers nobody can trust, silently.

## 9. Testing

Test domain packages directly. Mock IO at the **interface the consumer defined**, not at the concrete repo. Test handlers through the router with `httptest`. Test both stores against the same interface test. Full bar: `.claude/rules/go/testing-convention.md`.
