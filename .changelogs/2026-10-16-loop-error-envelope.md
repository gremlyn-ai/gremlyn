# 2026-10-16 — `loop` sends a valid response when the server answered with an error

**Type**: fix

**Summary**: On a JSON-RPC error response, the `loop` gremlin wrote its retry result next to the server's `error`, a response with both fields that JSON-RPC 2.0 forbids. It now replaces the error with the result.

**Impact**:
- Core (`pkg/…`): none
- Arena (`internal/arena/…`): `gremlins` (`LoopGremlin.Inject`)
- CLI / plugin / Action: none
- Formats: none
- Score comparability: the decisions to inject and the seeded stream are unchanged, so replays and stored events are unaffected. A live run against a server that answers errors can score differently from before, because the agent now receives a message its client accepts instead of an invalid one

**For users**: a `loop` scenario on a tool that fails now shows the agent the retry message instead of an envelope its client may reject.

**For developers**: the untargeted handler golden is renamed `internal/arena/chaos/testdata/untargeted.golden.jsonl` and changes on exactly one line, the `loop` run on an error response.

**Commits**: see the PR
