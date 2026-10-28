# 2026-10-28 — Strict `.gremlyn/arena.yaml`: a typo can no longer turn a scenario green

**Type**: fix (breaking for configs with unknown keys)

**Summary**: `arena ci` refuses an unknown key anywhere in the config, with its line and the closest known key, and refuses a `params` entry that no gremlin of the scenario reads. Before, `intesity: high` or `target_tool: [create_note]` was dropped silently and the scenario ran, and could pass, without its constraint. A free-form `meta:` map (top level and per scenario) holds the user's own data.

**Impact**:
- Core (`pkg/…`): none
- Arena (`internal/arena/…`): `chaos.BuildGremlinsWithParams` refuses a param none of the requested gremlins reads (`GremlinParams.unread`), which replaces the `injection_placement`-only check
- CLI / plugin / Action: `arena ci` config loading (`internal/cli/strictconfig.go`, `yaml.v3` `KnownFields`); `wrap --chaos-params` refuses unknown JSON fields; `thresholds.min_dimension` refuses an unknown dimension
- Formats: new optional `meta:` at the top level and in `scenarios[]`. Not in the report yet
- Score comparability: unchanged

**Migration**: a config that loaded under v0.3 may be refused. The error says what to do:
- `line 9: scenarios[0].intesity: unknown key, did you mean "intensity"?` → fix the spelling
- `scenarios[0].measures: unknown key (known: …); your own metadata goes under meta:` → move the key under `meta:`
- `scenario "x": payload_bytes set but no gremlin of this scenario reads it (read by overflow)` → delete the param, or add the gremlin
- `thresholds.min_dimension: unknown dimension "injection"` → use the dimension name, `injection_defense`

Every file in `examples/`, `.gremlyn/arena.yaml` and the `docs/demo` reports still loads (`TestShippedConfigsStillLoad`).

**For developers**: a new `GremlinParams` field gets its reading gremlins in `GremlinParams.unread`.
