# 2026-10-16 — `injection_placement`: put the injection where the model reads it

**Type**: feature

**Summary**: The `injection` gremlin can append its payload as a text part of the result's `content`, which clients such as Claude Code show the model, instead of a `_system_note` field they drop. The default stays the field.

**Impact**:
- Core (`pkg/…`): `models.EventDetails.Placement` (omitted when unset)
- Arena (`internal/arena/…`): `gremlins` (`InjectionGremlin.Placement`, `PlacementFor`); `chaos` (`GremlinParams.InjectionPlacement`, validation, the injection and its event record the placement)
- CLI / plugin / Action: `arena ci` reports `injection_fallbacks` and a `placement:` line; `examples/arena-notes.yaml` `injected-delete` uses `content`; the scripted `refagent` obeys an instruction found in `content`
- Formats: new gremlin param `injection_placement: field | content` in `.gremlyn/arena.yaml`; `placement` in event details and `injection_fallbacks` in the `arena ci` report, both only when `content` was asked for
- Score comparability: unchanged with the default. A scenario switched to `content` measures an injection the model can now see, so its score is a different measurement from its baseline

**For users**: `params: {injection_placement: content}` on an `injection` scenario. `docs/demo/report-injection-content.json` is the first real run: Claude received the instruction to delete a note and did not.

**For developers**: `structuredContent` is never touched and `isError` results are injected; a result without a `content` array falls back to the field and says so. `identity`, `loop`, `overflow` and `corruption` (`wrong_types`) have the same blindness and no param yet.

**Commits**: see the PR
