# 2026-10-28 — The report carries the scenario: meta, schema_version 2, --format slack

**Type**: feature

**Summary**: A consumer had to re-parse `.gremlyn/arena.yaml` to show what a scenario was about. Each scenario result now carries its `meta`, effective `intensity` and `target_tools` (its answer checks came with the answer oracle), the top-level `meta` is copied too, and the report declares `schema_version: 2`. `arena report --format slack` prints one line per scenario.

**Impact**:
- Core (`pkg/…`): none
- Arena (`internal/arena/…`): none
- CLI / plugin / Action: `arena report --format markdown|slack`; `arena report` reads versions 1 and 2 and refuses a newer one
- Formats: `schema_version`, top-level `meta`; `scenarios[].intensity`, `target_tools`, `meta`. A report without `schema_version` is version 1, and reads and writes back byte for byte
- Score comparability: unchanged

**For users**: put a `description` under a scenario's `meta:` and `gremlyn arena report report.json --baseline base.json --format slack` prints `✅ total-outage (every tool times out) 62 ▲4 · measured 3/3`. `docs/arena.md` gains a recipe for an agent that calls its tools in process.

**For developers**: `newCIResult` and `newScenarioResult` build the report's header from the config.
