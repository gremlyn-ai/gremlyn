# Gremlyn Development Workflow

Drive any feature, bug, or non-trivial change through the standard Gremlyn pipeline defined in [.claude/workflow.md](../workflow.md): reflection → architecture → dev → review → fix → QA → user test → docs. Each step routes to the right specialist agent and **pauses** for an explicit user verdict before the next.

## Input

`$ARGUMENTS` — a fuzzy request, feature idea, bug report, or ticket. If empty, ask the user what they want to build or fix before starting Step 1.

## How to run it

Treat [.claude/workflow.md](../workflow.md) as the source of truth. Execute it **one step at a time**, never auto-advancing past a `⏸ PAUSE`.

### ⚠️ First, establish the area scope
Gremlyn is **one git repository, one Go module** (`github.com/gremlyn-ai/gremlyn`) plus `dashboard/`. Before Step 1, name the areas in scope (`pkg/`, `internal/shield/`, `internal/arena/`, `internal/cli/`, `migrations/`, `dashboard/`). If **`pkg/`** is involved, it is still the highest-blast-radius change — it ripples into both services and the CLI — but it is **one ticket, one commit**: the consumers migrate in the same diff, and `make check` at the root is what proves it. State this up front.

### Step 1 — 🧠 Reflection (always)
- Dispatch `product-manager` (co-own with `mcp-domain-expert` when the question is "what is the right behavior", `frontend-architect` if UI-heavy, `database-engineer` if schema-heavy).
- Produce the markdown ticket + a `Routing summary:` block (format at the bottom of workflow.md).
- Split **only** if the expected diff > 400 LOC — never because the change spans `pkg/` and a service. If a critical open question remains → **PAUSE and ask**.
- ⏸ Present the ticket. Wait for validation before Step 2.

### Step 2 — 🏛️ Architecture (conditional)
- Invoke for: a `pkg/` or pipeline-contract change, a new package/route/store, non-trivial schema, a new detection layer or score dimension, hot-path perf work. Otherwise state "skip" and go to Step 3.
- Owner per the nature table in workflow.md (`proxy-engine-developer` / `detection-pipeline-engineer` / `chaos-gremlin-designer` / `database-engineer` / `frontend-architect` / `ai-engineer` / `release-infrastructure` / `mcp-domain-expert`).
- Deliver an architecture brief (signatures, file layout, migration plan — no full code).
- ⏸ Validate the brief before any code.

### Step 3 — 🛠️ Development
- Owner auto-detected from the files touched (dev agent table in workflow.md). Invoke the skills named in the ticket (`/new-endpoint`, `/new-gremlin`, `/new-detection-rule`, `/dashboard-from-reference`, `/optimize-endpoint`).
- Tests written **alongside**, not after.
- Gate before "done": Go → `make check` green at the repo root, **once** (it covers `pkg/`, both services and the CLI). Dashboard → `make dashboard-check` = typecheck + lint + `vitest run` + **`npm run build`**.
- Conventional Commits. One branch, `feat/<slug>` or `fix/<slug>`.
- Hard rules: no CGO · no `map[string]interface{}` for a known shape · no `any` in TS · schema changes in **both** stores · a detection change ships **negative** corpus cases · no hardcoded secrets · deps Apache-2.0/MIT/BSD only.

### Step 4 — 👀 Review
- Always `code-reviewer`. Add co-reviewers per what the diff touches (co-reviewer table in workflow.md): `security-reviewer` (detection/policy/auth/SQL/PII — and always for a Shield change), `proxy-engine-developer` (`pkg/proxy`, `pkg/protocol`), `detection-pipeline-engineer`, `chaos-gremlin-designer`, `database-engineer`, `frontend-architect`, `mcp-domain-expert`, `release-infrastructure`, `legal-compliance-checker`.
- Use `/review-uncommitted` (local diff) or `/review` (PR).
- Verdict: `ship` → Step 5 · `fix-and-ship` → Step 4bis → Step 5 · `block` → back to Step 3.

### Step 4bis — 🔧 Fix
- Same dev agent from Step 3. Only the review findings, no scope creep. New commit (never amend without an explicit request). Loop 4 → 4bis until `ship`.

### Step 5 — ✅ Automated Tests
- `qa-engineer`. Cover the ticket's acceptance criteria (70/20/10 unit/integration/e2e).
- `make check` at the root — `-race` is mandatory, and a `-race` report is never treated as a flake. Integration: `docker compose up -d postgres redis && make integration`. Dashboard: `make dashboard-check`.
- **If the proxy path changed**: e2e against a real MCP server — `./bin/gremlyn wrap -- npx -y @modelcontextprotocol/server-memory`, exercise `initialize`/`tools/list`/`tools/call`.
- Failure → Step 4bis. Deliver green suites + the manual validation checklist.

### Step 6 — 👤 User Test
- Hand over the checklist. ⏸ **LONG PAUSE** — wait for "OK validated". Functional bug → Step 3 (or Step 1 if the AC was wrong). Visual bug → `frontend-nextjs-developer` (+ `ui-designer` / `whimsy-injector`).

### Step 7 — 📚 Documentation
- `documentation-writer` (co-own `analytics-reporter` for a new metric, `brand-guardian` for user-facing copy). Update the right `docs/` page (`core.md` / `shield.md` / `arena.md` / `dashboard.md`), add `.changelogs/<YYYY-MM-DD>-<slug>.md`, update the root `CLAUDE.md` **and** the matching `.claude/rules/` file if a convention changed, verify links resolve. A `pkg/` change is marked `breaking` only if user-visible behavior changed — there is no version chain to write out.

## Rules (refuse if violated)
🚫 Skipping Step 1 · 🚫 diff > 400 LOC without a split · 🚫 tests after code · 🚫 bare `go test` instead of `-race` · 🚫 dismissing a `-race` report as flaky · 🚫 a detection change with positives only · 🚫 a schema change in one store only · 🚫 adding CGO · 🚫 `any` in TS · 🚫 ignoring a `ship` verdict to loop again · 🚫 push before user validation · 🚫 deferring docs · 🚫 amending a published commit or moving a published tag without a request · 🚫 hardcoding a secret.

## Special cases (see workflow.md for the full sequences)
- **Core `pkg/` change**: one ticket, contract brief, `pkg/` + both consumers + CLI call sites in the **same commit**, `make check` at the root proves the consumers, e2e against a real MCP server.
- **Detection change**: AC includes a false-positive budget; negative corpus mandatory including the meta-corpus class; `security-reviewer` always; report precision/recall + FP rate.
- **New gremlin / score dimension**: PM + `mcp-domain-expert` define what real failure it models and what a resilient agent does; contract compliance is the checklist; a weight change breaks score comparability.
- **Dashboard-only**: read the matching `reference/*.html` first; all four states (empty, loading, per-section error, live) required; `npm run build` must pass.
- **Internal refactor**: baseline numbers first, one change at a time, `benchstat` for significance, determinism and purity re-verified.
- **Release**: one tag for the module, cross-compile matrix, checksums for all three binaries, clean-clone `make build`, zero-config path verified.

Start now with Step 1 for: $ARGUMENTS
