---
paths:
  - "**/*.go"
  - "**/go.mod"
  - "**/go.sum"
  - "**/.golangci.yml"
  - "**/Makefile"
---

# Lint, Vet & Test Gate — Go

## 1. The command

```bash
make check          # = vet + lint + test. THE gate. Run this before every commit.
```

Sub-targets when you need them piecemeal:
- `make vet` — `go vet ./...`
- `make lint` — `golangci-lint run`
- `make test` — `go test ./... -race -coverprofile=coverage.out`
- `make coverage` — HTML coverage report
- `make build` — all three binaries (`bin/gremlyn`, `bin/shield`, `bin/arena`)
- `make integration` — the `//go:build integration` suite
- `make tidy` — `go mod tidy`
- `gofmt -l .` / `goimports -l .` — list unformatted files

One Makefile at the repo root covers the whole module: `cmd/{gremlyn,shield,arena}`, every `pkg/` and every `internal/` package. If you find yourself running raw `go` commands to build or test a subtree, that's a bug — fix the Makefile.

## 2. The bar

- **`gofmt` and `goimports` are mandatory.** No style debates. Formatting is automated on save via the `PostToolUse` hook in `.claude/settings.json`; if a file arrives unformatted, format it, don't argue.
- **`go vet ./...` must be silent.**
- **`golangci-lint run` must be clean** — zero findings, not "only minor ones". The enabled linters are: `errcheck, gosimple, govet, ineffassign, staticcheck, unused, gofmt, goimports, misspell, unconvert, gocritic, revive`.
- **`go test -race ./...` must pass.** `-race` is not optional in this codebase — see §4.

One `.golangci.yml` at the root governs the whole module — `pkg/`, `internal/cli`, `internal/shield`, `internal/arena` are held to the same rules. Don't add a per-directory exclusion to get a diff through; fix the code.

## 3. Fixing common lint friction (don't reach for blanket ignores)

- **`errcheck` on a deliberately-ignored error** — the answer is almost never to ignore it. `defer resp.Body.Close()` genuinely can't be handled: write `defer func() { _ = resp.Body.Close() }()`. For anything else, handle or wrap it. In a security product a swallowed error is how a policy check silently doesn't run.
- **`revive: exported`** — every exported symbol needs a godoc comment starting with its own name. This is a project convention (`docs/core.md`), not just a linter preference. Write the contract: preconditions, returned error values, concurrency safety.
- **`unused` on a type you're about to use** — don't silence it, land the consumer in the same commit. Dead code in a security tool is unreviewed attack surface.
- **`gocritic` on a large value copied in a range loop** — take the index or a pointer. On hot paths (`pkg/proxy`, `internal/detection`) this is a real allocation cost, not a nit.
- **`staticcheck SA1019` (deprecated)** — migrate. Don't pin to a deprecated API to skip a rename.
- **`unconvert`** — remove the redundant conversion; if the conversion looked necessary, the underlying type is probably wrong.
- **`ineffassign`** — usually a real bug: an error assigned and then overwritten before it's checked.

**A `//nolint` needs a one-line reason on the same line and must be as narrow as possible** (`//nolint:gosec // path is a validated internal constant`). A bare `//nolint` is a review block. Never `//nolint` in `internal/shield/detection/` or `internal/shield/policy/` without `security-reviewer` sign-off — that's the code the product exists for.

## 4. `-race` is part of the gate, not an extra

This codebase is concurrent by design: the proxy runs full-duplex goroutines per connection, Arena runs concurrent sessions each with its own proxy instance, and Shield decides policy under concurrent load.

- `make test` already includes `-race`. Never run bare `go test` and call it green.
- Hunting a flake: `go test -race -count=3 ./...`. A test that only passes cached isn't passing.
- **A `-race` report is never dismissed as flaky.** A data race in a policy decision can produce an `allow` where a `block` was computed — that's a vulnerability, not a test artifact. Fix it, and loop in `security-reviewer` if it's on a decision path.

## 5. Benchmarks on the hot paths

Any change to `pkg/proxy`, `pkg/protocol`, or `internal/shield/detection` ships with numbers:

```bash
go test -bench=. -benchmem ./pkg/proxy/
go test -bench=. -benchmem -count=10 ./pkg/proxy/ > new.txt && benchstat old.txt new.txt
```

Report `ns/op`, `B/op`, `allocs/op` before and after. Use `benchstat` — a delta between two single runs is noise, not a result.

## 6. One module, one gate

Everything lives in `github.com/gremlyn-ai/gremlyn`: no `replace` directive, no version-bump chain, no per-repo run.

- **`make check` at the root compiles and tests `pkg/`, `internal/cli`, `internal/shield` and `internal/arena` together.** A `pkg/` change that breaks a consumer fails the same command that runs the change's own tests — so a `pkg/` change and its consumer updates belong in **one commit**, not a sequence of them.
- `make tidy` after touching imports. A stale `go.mod`/`go.sum` is a broken build for the next clone.
- `make dashboard-check` when the diff touches `dashboard/` — the Go gate says nothing about TypeScript.
- `make integration` needs `docker compose up -d postgres redis`; `make check` must stay green **without** them — zero-config is a product feature.

## 7. Non-negotiables (a violation blocks the diff)

- No `map[string]interface{}` for a known shape.
- Every error wrapped with context (`fmt.Errorf("op: %w", err)`); none discarded.
- `ctx context.Context` first param on every IO function; no `context.TODO()` shipped.
- Every SQL query parameterized (`?` SQLite / `$1` pgx). No string concatenation into SQL, ever.
- No global mutable state, no init-time side effects.
- No `fmt.Println` / `log.Printf` — zerolog only.
- **No CGO.** `CGO_ENABLED=0` must keep working; storage is pure-Go SQLite precisely so static cross-compiled binaries ship. A CGO dependency is a blocking decision, not a lint fix.
