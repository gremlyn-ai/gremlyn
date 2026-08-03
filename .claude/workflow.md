# Gremlyn Development Workflow

> Standard execution pipeline for any feature, bug, or non-trivial change across the four Gremlyn repos (`gremlyn-core`, `gremlyn-shield`, `gremlyn-arena`, `gremlyn-dashboard`).
>
> Each step routes automatically to the right specialist agent. Agents live in [`.claude/agents/`](agents/).

---

## Overview

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│ 1. Reflection   │ ──▶ │ 2. Architecture │ ──▶ │ 3. Development  │
│ (PM)            │     │ (optional)      │     │ (specialists)   │
└─────────────────┘     └─────────────────┘     └─────────────────┘
                                                         │
                                                         ▼
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│ 7. Documentation│ ◀── │ 6. User Test    │ ◀── │ 4. Review + Fix │
│                 │     │ (manual         │     │ 5. Automated QA │
│                 │     │  validation)    │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

Mandatory pause between each step — an agent never triggers the next one without an explicit verdict.

---

## ⚠️ The multi-repo rule that governs everything

The four repos are **independent git repositories**. There is no monorepo, no atomic cross-repo commit.

`gremlyn-shield` and `gremlyn-arena` import `github.com/gremlyn-ai/gremlyn/pkg/...` (local dev via a `replace` directive in `go.mod`).

**Therefore:**

- A change touching **`pkg/`** is **always at least two tickets**: core first, then each consumer. It cannot be one PR.
- The version order is fixed and non-negotiable:
  1. change + test + tag **`gremlyn-core`**
  2. `go get github.com/gremlyn-ai/gremlyn-core@vX.Y.Z` in **each** consumer, build, test
  3. tag the consumers
- After any core change, `make check` runs in **all three** Go repos. Core passing its own tests proves nothing about its consumers.
- Always state **which repo** a command runs in. Wrong working directory is the most common mistake in this project.

---

## Step 1 — 🧠 Reflection

**Owner**: `product-manager`
**Possible co-owners**:
- `mcp-domain-expert` if the question is *"what is the right behavior"* (protocol semantics, what an action should do, what counts as resilient)
- `frontend-architect` if scope is mostly UI
- `database-engineer` if scope is mostly schema

**Goal**: turn a fuzzy request into an executable ticket.

**Procedure**:
1. PM clarifies problem, scope, acceptance criteria.
2. PM identifies impacted repos and packages. **A `pkg/` change in core is flagged loudly.**
3. PM proposes the **routing** (which agents own each next step).
4. PM lists risks / open questions.

**Deliverable**: a markdown ticket in the format defined in [`agents/product-manager.md`](agents/product-manager.md), ending with a `Routing summary: …` line.

**Rules**:
- No code written at this step.
- If > 400 LOC of expected diff, the ticket MUST be split.
- If the change spans core + a service, it MUST be split.
- If a critical open question remains, **PAUSE**, ask the user.

> ⏸ **PAUSE**: validate the ticket with the user before Step 2.

---

## Step 2 — 🏛️ Architecture (conditional)

**When to invoke**:
- A `pkg/` API or pipeline-contract change.
- New package, new top-level route, new store.
- Non-trivial schema or a new high-volume table.
- A new detection layer or a new score dimension.
- Performance work at the proxy hot path.

**Otherwise**: skip directly to Step 3.

**Owner** (depending on nature):
| Nature | Agent |
|--------|-------|
| Proxy engine, JSON-RPC, pipeline contract, transports | `proxy-engine-developer` |
| Detection layers, policy semantics, behavioral monitoring | `detection-pipeline-engineer` |
| Gremlins, scoring model, session semantics | `chaos-gremlin-designer` |
| Schema, indexes, retention, risky migrations | `database-engineer` |
| Dashboard structure, routes, stores, WebSocket architecture | `frontend-architect` |
| LLM-as-judge, ML sidecar, MCP protocol depth | `ai-engineer` |
| Build, release, CI, cross-repo versioning | `release-infrastructure` |
| Behavior / semantics ("what should this DO") | `mcp-domain-expert` |

**Deliverable**: an architecture brief (signatures, file layout, migration plan — no full code).

> ⏸ **PAUSE**: validate the brief with the user before code.

---

## Step 3 — 🛠️ Development

**Owner**: auto-detected from the files the ticket touches.

| Scope | Agent |
|-------|-------|
| `pkg/proxy/`, `pkg/protocol/` — the hot path | `proxy-engine-developer` |
| General Go: handlers, services, repos, CLI, config, migrations | `go-backend-developer` |
| `shield/internal/{detection,policy,behavioral}/` | `detection-pipeline-engineer` |
| `arena/internal/{gremlins,scoring,session}/` | `chaos-gremlin-designer` |
| Dashboard components, pages, stores, API client | `frontend-nextjs-developer` |
| Risky migration / schema work | `database-engineer` |
| Makefile, Dockerfile, CI, `.golangci.yml`, release | `release-infrastructure` |
| LLM-as-judge prompt, ML sidecar contract | `ai-engineer` |
| Measurement study (detection quality, score validity) | `data-scientist` |

**Skills to invoke when relevant**: `/new-endpoint`, `/new-gremlin`, `/new-detection-rule`, `/dashboard-from-reference`, `/stack-health`, `/optimize-endpoint`.

**Rules**:
- **Tests written alongside**, not after. See `qa-engineer` for the patterns.
- Before marking done:
  - Go: `make check` green (= `go vet` + `golangci-lint` + `go test -race`) in **every** repo touched.
  - Dashboard: `npm run typecheck` + `npm run lint` + `npx vitest run` + **`npm run build`** all clean.
- Conventional Commits on every commit, **per repo**.
- Target diff < 400 lines (otherwise split — refuse and escalate to PM).
- **No CGO.** `CGO_ENABLED=0` must keep working.
- No hardcoded secrets. No dependency outside Apache-2.0 / MIT / BSD without explicit user validation.
- No `map[string]interface{}` for a known shape; no `any` in TypeScript.
- A detection change ships **negative** corpus cases, not only positives.
- A schema change lands in **both** SQLite and PostgreSQL.

**Deliverable**: code + tests on branch `feat/<slug>` or `fix/<slug>`, **in each affected repo**.

---

## Step 4 — 👀 Review

**Owner**: `code-reviewer` (always).

**Co-reviewers** (triggered by what the diff touches):
| If the diff touches… | Co-reviewer |
|------------------------|-------------|
| `internal/detection/`, `internal/policy/`, auth, SQL, PII, secrets | `security-reviewer` |
| `pkg/proxy/`, `pkg/protocol/` | `proxy-engine-developer` |
| `internal/{detection,policy,behavioral}/` | `detection-pipeline-engineer` |
| `internal/{gremlins,scoring,session}/` | `chaos-gremlin-designer` |
| Migration, schema, index, a query on a growing table | `database-engineer` |
| Route layout, store, WebSocket architecture, shared component | `frontend-architect` |
| Behavior/semantics questions raised in review | `mcp-domain-expert` |
| Makefile, Dockerfile, CI, `.golangci.yml`, release scripts | `release-infrastructure` |
| PII handling, payload retention, data export, a new sub-processor | `legal-compliance-checker` |

**Procedure**: invoke `/review-uncommitted` (local diff) or `/review` (PR). `code-reviewer` then aggregates the co-reviewer angles.

**Deliverable**: severity report 🔴 CRITICAL / 🟠 HIGH / 🟡 MEDIUM / 🟢 LOW + verdict:
- `ship` → Step 5
- `fix-and-ship` → Step 4bis, then Step 5
- `block` → back to Step 3

> ⏸ **SHORT PAUSE**: if verdict ≠ `ship`, run Step 4bis before continuing.

---

## Step 4bis — 🔧 Fix

**Owner**: the same dev agent who wrote the code in Step 3.

**Rules**:
- Address **only** the review findings. No scope expansion.
- New commit (never amend without an explicit user request).
- Re-run the local gate after each fix.

> Loop 4 → 4bis until the verdict is `ship`.

---

## Step 5 — ✅ Automated Tests

**Owner**: `qa-engineer`.

**Coverage target**: 70% unit / 20% integration / 10% e2e.

**Procedure**:
1. Verify the tests written in Step 3 cover the ticket's acceptance criteria.
2. Fill gaps (Go table-driven, Vitest, integration).
3. Run the suites:
   - Go, per repo: `go test -race ./...` — and `-count=2` when hunting a flake.
   - Integration where relevant: `docker compose up -d postgres redis && go test -tags=integration ./...`
   - Dashboard: `npm run typecheck && npm run lint && npx vitest run && npm run build`
4. **End-to-end against a real MCP server** whenever the proxy path changed:
   `./gremlyn wrap -- npx @modelcontextprotocol/server-memory` → exercise `initialize`, `tools/list`, `tools/call`.
5. Failure → back to Step 4bis with the relevant dev.

**Deliverable**: green suites + the **manual validation checklist** (template in [`agents/qa-engineer.md`](agents/qa-engineer.md)) handed to the user.

---

## Step 6 — 👤 User Test (manual validation)

**Owner**: the human user.

**Procedure**:
1. QA provides the markdown checklist.
2. User brings up the stack (3 terminals: shield :8081, arena :8082, dashboard :3000) and runs golden path + edge cases + regression spots.
3. Until the checklist is signed off, do not proceed to Step 7.

**Failure cases**:
- Functional bug → back to Step 3 (dev fix) or Step 1 (if the AC was wrong).
- Visual / UX bug → `frontend-nextjs-developer` (+ `ui-designer` / `whimsy-injector`).

> ⏸ **LONG PAUSE**: wait for user validation ("OK validated").

---

## Step 7 — 📚 Documentation

**Owner**: `documentation-writer`.

**Possible co-owners**:
- `analytics-reporter` if the change creates a new user-facing metric.
- `brand-guardian` if it introduces user-facing copy or terminology.

**Procedure**:
1. Read the final diff + the original ticket.
2. Update the right `<repo>/docs/<Area>/` page.
3. Create the changelog entry in `.changelogs/<YYYY-MM-DD>-<slug>.md`. **A core `pkg/` API change is always marked `breaking`** and names the consumer versions needing a bump.
4. If a convention changed → update the owning `<repo>/CLAUDE.md` **and** the matching `.claude/rules/` file (they must not disagree). Keep each CLAUDE.md under ~150 lines.
5. Add godoc for any new exported Go symbol.
6. Verify links resolve — cross-repo links must name the repo, since each is cloned independently.

**Deliverable**: docs commits, per repo.

---

## Routing summary (for automatic parsing)

Format the PM provides at the end of the ticket:

```
Routing summary:
- Repos: <core / shield / arena / dashboard>
- Reflection: product-manager [+ mcp-domain-expert]
- Architecture: <agent or "skip">
- Development: <main dev agent> [+ co-devs]
- Review: code-reviewer + <co-reviewers>
- QA: qa-engineer
- User Test: user
- Docs: documentation-writer
```

---

## Agents × Steps recap

| Agent | Primary step | Secondary steps |
|-------|--------------|-----------------|
| `product-manager` | 1 | — |
| `mcp-domain-expert` | 1, 2 | review (semantics) |
| `frontend-architect` | 2 | review (structure) |
| `database-engineer` | 2 | 3 (risky migration), review (DB) |
| `proxy-engine-developer` | 2, 3 | review (core `pkg/`) |
| `detection-pipeline-engineer` | 3 | 2 (layer design), review |
| `chaos-gremlin-designer` | 3 | 2 (gremlin/score design), review |
| `go-backend-developer` | 3 | 4bis |
| `frontend-nextjs-developer` | 3 | 4bis, 6 (visual fixes) |
| `ai-engineer` | 3 | 2 (L2/L3 design) |
| `release-infrastructure` | 3 | 2 (build design), 7 (release notes) |
| `data-scientist` | — | measurement studies, any quantitative claim |
| `code-reviewer` | 4 | — |
| `security-reviewer` | 4 (co) | — |
| `legal-compliance-checker` | 4 (co) | — |
| `qa-engineer` | 5 | — |
| `api-tester` | 5 (functional/security/perf) | — |
| `performance-benchmarker` | 5 (load) | 2 (perf design) |
| `test-results-analyzer` | 5 (post-mortem) | — |
| `refactor-perf-engineer` | 3 (perf/refactor tickets) | — |
| `documentation-writer` | 7 | — |
| `analytics-reporter` | 7 (co) | 2 (metric definition) |
| `infrastructure-maintainer` | — | diagnostics, incidents |
| `ui-designer` / `ux-researcher` / `brand-guardian` / `whimsy-injector` / `visual-storyteller` | 6 (UX feedback) | 2 (front design) |
| `tool-evaluator` | — | any new dependency decision |
| `workflow-optimizer` | — | process/automation improvements |

---

## Anti-patterns to refuse

- 🚫 Skipping Step 1 ("just code it").
- 🚫 **A core `pkg/` change and its consumer updates in one ticket.**
- 🚫 **Tagging a consumer before core.**
- 🚫 Diff > 400 LOC without a split.
- 🚫 Tests written after the code.
- 🚫 Running bare `go test` instead of `-race`.
- 🚫 Dismissing a `-race` report as flaky.
- 🚫 A detection change with positive cases only.
- 🚫 A schema change in one store only.
- 🚫 Adding a CGO dependency.
- 🚫 `any` in TypeScript, `map[string]interface{}` for a known Go shape.
- 🚫 Ignoring a `ship` verdict to loop again.
- 🚫 Push before user validation (Step 6).
- 🚫 Deferring documentation to "later".
- 🚫 Amending a published commit, or moving a published tag, without an explicit request.
- 🚫 Hardcoding a secret "temporarily".

---

## Special cases

### Core `pkg/` change (the most common trap)
- Step 1: PM splits into **N+1 tickets** — one for core, one per consumer.
- Step 2: `proxy-engine-developer` writes the contract brief. Ordering of pipeline stages and exported signatures are stated explicitly.
- Step 3: core only. Tests + benchmark. Tag.
- Step 3b: each consumer — `go get …@vX.Y.Z`, build, test, tag.
- Step 4: `code-reviewer` + `proxy-engine-developer` on core; then per consumer.
- Step 5: `make check` in all three repos + e2e against a real MCP server.
- Step 7: changelog marked `breaking`, with the version chain written out.

### Detection change (new rule / pattern / layer)
- Step 1: short ticket, AC includes **a false-positive budget**, not just "catches X".
- Step 3: `/new-detection-rule` skill. Positive **and** negative corpus cases mandatory — including the meta-corpus class (docs about injection must not match).
- Step 4: `code-reviewer` + `security-reviewer` (always — check the fail-open matrix).
- Step 5: report precision/recall before and after, with the FP rate. `data-scientist` if a number will be published.
- Step 6: run against real MCP traffic and confirm nothing legitimate broke.

### New gremlin / score dimension
- Step 1: PM + `mcp-domain-expert` — what real failure does it model, what does a resilient agent do.
- Step 3: `/new-gremlin` skill. Contract compliance is the checklist: no-op on not-injected, deterministic under seed, bounded, ctx honoured.
- Step 4: `code-reviewer` + `chaos-gremlin-designer`.
- Step 7: a weight change must be flagged as breaking historical score comparability.

### Dashboard-only change
- Step 1: short ticket.
- Step 2: `frontend-architect` if it adds a route or a store; else skip.
- Step 3: `/dashboard-from-reference` skill; read the matching `reference/*.html` first.
- Step 5: typecheck + lint + vitest + **build**.
- Step 6: visual QA mandatory — all four states (empty, loading, per-section error, live).

### Bug fix
- Step 1: short ticket (`fix/<slug>`), AC = repro ✅ + fix ✅ + regression test ✅.
- Step 2: skip.
- Step 3: minimal fix + the regression test that would have caught it.
- Step 4: `code-reviewer` + `security-reviewer` if it touched a decision path.
- Step 5: regression tests + the relevant suite.
- Step 7: changelog entry.

### Internal refactor (no visible behavior change)
- Step 1: very short ticket, AC = "behavior identical, gate green".
- Step 3: `refactor-perf-engineer` — baseline numbers first, one change at a time.
- Step 5: before/after numbers with `benchstat`; determinism and purity re-verified.
- Step 6: skip user test if genuinely nothing visible — say so explicitly.
- Step 7: only if a convention changed.

### Release
- Step 3: `release-infrastructure` — version chain, cross-compile matrix, checksums.
- Step 5: clean-clone build (no `replace`), `CGO_ENABLED=0` verified, `gremlyn version` reports correctly, zero-config path works with no compose services running.
- Step 7: release notes; flag any migration that will stall user startup.
