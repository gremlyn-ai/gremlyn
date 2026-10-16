# 2026-10-16 — `target_tools`: aim a scenario's gremlins at named tools

**Type**: feature

**Summary**: A scenario can name the tools its gremlins fire on, so a fault meant for a write lands on the write even when the agent reads first. The report says when a target was never called, never injected, or not offered by the server.

**Impact**:
- Core (`pkg/…`): none
- Arena (`internal/arena/…`): `chaos` (the handler filter, per-target counts, the `tools/list` union, `ValidateTargets`, `ResolveTargets`); `gremlins` (the unused `LatencyGremlin.TargetTool` field removed)
- CLI / plugin / Action: `arena ci`, `arena replay`, `wrap --chaos-target-tool` (repeatable); `skills/chaos-test` mentions the key; the PR comment gains a **Targets** line on a failing targeted scenario
- Formats: new `scenarios[].target_tools` in `.gremlyn/arena.yaml`; `target_tools` in the session recipe and `summary.json`, `targets` in `summary.json` and in `scenarios[]` of the `arena ci` report. All additive and omitted when the key is unset
- Score comparability: unchanged for every scenario without the key; a v0.2.0 golden and both `docs/demo` reports prove it. Adding the key to an existing scenario changes which call is injected, so its first delta against `main` compares two different measurements

**For users**: `target_tools: [create_note]` in a scenario. Names are the server's own, exact and case-sensitive. A target the agent never called fails the scenario. `examples/arena-notes.yaml` `timeout-on-create` now uses it, with a prompt that lists the notes first.

**For developers**: targeting lives in `chaos.GremlinHandler` only; gremlins stay unaware of it and non-target calls draw no randomness. `internal/arena/chaos/testdata/untargeted-v0.2.0.golden.jsonl` pins the untargeted handler output; regenerate it only on purpose, with `-update`.

**Commits**: #24, #25, #26
