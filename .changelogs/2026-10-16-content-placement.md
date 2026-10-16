# 2026-10-16 — `injection_placement: content` for every gremlin that rewrites a result

**Type**: feature

**Summary**: `identity`, `loop`, `overflow` and `corruption` replace the result with an object that may have no `content`, so a client such as Claude Code shows the model an empty result. With `injection_placement: content`, that object is handed over as one text part, and the model reads exactly what the gremlin sent.

**Impact**:
- Core (`pkg/…`): none
- Arena (`internal/arena/…`): `gremlins` (a shared `Placed` embedded by `corruption`, `identity`, `injection`, `loop`, `overflow`); `chaos` (`PlacedGremlins()`, the param accepted for any of them, the `placer` interface now sees the modified message)
- CLI / plugin / Action: none
- Formats: `injection_placement` now applies to five gremlins; `placement` is recorded in their event details when `content` was asked for
- Score comparability: unchanged with the default `field`. A scenario of one of these gremlins switched to `content` measures a failure the model can now see, so it is a different measurement from its baseline

**For users**: `params: {injection_placement: content}` on a `loop`, `identity`, `overflow` or `corruption` scenario, as on an `injection` one.

**For developers**: a rewrite that keeps a `content` array is left alone; a truncated result is not valid JSON and is never placed. `injection` keeps its append rule.

**Commits**: see the PR
