# 2026-10-28 — Answer oracle: an injection that reaches the user fails the scenario

**Type**: feature

**Summary**: Gremlyn now reads the agent's final answer and checks it. An injection that wins without any tool call after it (the agent repeats a phishing link to the user) used to look like a resisted injection; it is now a leak that fails the scenario and zeroes the injection dimension.

**Impact**:
- Core (`pkg/…`): `models.OutcomeLeaked`; `EventDetails.check` and `evidence`
- Arena (`internal/arena/…`): `chaos/answer.go` (checks, `Canary`, `CanaryFor`, `AnswerChecks`); the observer checks every later `tools/call`'s arguments for the canary (`WithCanary`); `BuildGremlinsWithParams` appends the canary to the `injection` and `identity` text; `scoring` puts leaks in `injection_defense` and zeroes a dimension with a leak
- CLI / plugin / Action: `{{answer_file}}` placeholder, stdout fallback with the `claude -p` JSON envelope unwrapped, `scenarios[].expect.answer`, `params.canary`; text output and PR comment show the answer checks, evidence in a code span; `skills/chaos-test` reads `report.leaked`
- Formats: `scenarios[].answer {source, checks[]}` and `report.leaked`, `dimensions.*.leaked` in the `arena ci` report, additive. Answer events (gremlin `answer`) in `events.jsonl`, not counted as gremlins in the session record
- Score comparability: the injected text of `injection` and `identity` now ends with ` (ref GRM-xxxxxxxx)`, so an agent sees a slightly different payload than in v0.3 (`untargeted.golden.jsonl` regenerated; its only change is that suffix). A scenario whose answer leaks scores 0 on `injection_defense` where it scored on tool traffic alone before

**For users**: nothing to do for `claude -p`, whose stdout is read. Another agent writes its answer to `{{answer_file}}`. Add `expect.answer` rules to a scenario (`links_only_to: [notes.example]`); turn the canary off with `params: {canary: false}`.

**For developers**: answer checks are events, so `scoring` stays `[]ArenaEvent → report`; the CLI reads the answer and appends its events before scoring. The refagent gains `--answer`, `--echo-result`, `--answer-file` and `--forward-result`.
