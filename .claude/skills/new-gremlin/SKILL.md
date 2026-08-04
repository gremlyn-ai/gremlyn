---
name: new-gremlin
description: Add a new chaos gremlin to Arena end to end — the Gremlin interface implementation, registry entry, bounds and config, scoring dimension mapping, table-driven + determinism tests, and the dashboard selector metadata. Use when the user says "new gremlin", "add a failure injector", "inject X into the pipeline", or runs /new-gremlin.
---

# New Gremlin

Add a failure injector to Arena. A gremlin is not just a function that mangles a message — it's a **measurement instrument**, and the contract below is what makes its output trustworthy.

## Arguments

- `name`: e.g. `TruncationGremlin`
- `failure`: the real-world failure it models
- `dimension`: which resilience dimension it stresses

## Step 0 — Answer these before writing code

If you can't answer all five, stop and route to `mcp-domain-expert`. A gremlin that fails these is noise in the score.

1. **What real failure does this model?** `HallucinationGremlin` = a lying tool. `IdentityGremlin` = a spoofed tool. If you can't name the real-world failure, don't ship it.
2. **Which resilience dimension does it stress?** If none, either add the dimension (with its recorder change) or drop the gremlin.
3. **What SHOULD a resilient agent do?** This is the scoring rubric — write it down, it becomes the score's meaning.
4. **What's the observable signal in the recorded events?** If a good and a bad agent reaction look identical in the event stream, the recorder needs a change and that's part of this work.
5. **What are the bounds?** Probability plus gremlin-specific limits. Defaults must be safe.

## Files to touch

```
internal/arena/gremlins/<name>.go              → the implementation
internal/arena/gremlins/gremlins_test.go       → table-driven tests
internal/arena/gremlins/scenarios_test.go      → cross-component behavior
internal/arena/gremlins/registry.go            → register + metadata the dashboard renders
internal/arena/scoring/dimensions.go           → only if a new dimension is needed
internal/arena/session/recorder.go             → only if a new event field is needed
Arena.yaml / config              → the knobs
─────────────────────────────────────────────────────────────────
dashboard/lib/api/types.ts       → if the gremlin DTO shape changed
```

The dashboard's `GremlinSelector.tsx` renders **from the registry metadata**, not from hardcoded frontend strings. Get the metadata right and the UI follows.

## The contract — every item is mandatory

```go
Inject(ctx context.Context, msg Message) (modified Message, injected bool, err error)
```

- [ ] **`injected == false` is a byte-exact no-op.** The returned message is the input, untouched. Not "mostly the same", not normalized. A gremlin that normalizes on the not-injected path contaminates every control run.
- [ ] **Probability-gated, seeded, deterministic.** Same seed + same message sequence → same injections. A session that can't be replayed can't be debugged, and a score that isn't reproducible isn't a score.
- [ ] **Never break the JSON-RPC envelope** — unless breaking it IS the declared purpose, and then it's stated in the doc comment AND the registry description. Corrupting a *payload* is the product; dropping an `id` hangs the agent and looks like a gremlin bug.
- [ ] **Stateless across sessions.** All state in the session. Arena instantiates a proxy per session — two concurrent sessions must not see each other.
- [ ] **Bounded.** Max size, max delay, max iterations — finite, configured, documented. An unbounded gremlin wedges the run instead of testing resilience.
- [ ] **Honours `ctx`.** A cancelled session stops waiting immediately, not after the sleep. Critical for latency- and timeout-shaped gremlins.
- [ ] **Self-describing.** Name, description, stressed dimension(s), config knobs — exposed via the registry.

## Template

```go
package gremlins

// TruncationGremlin cuts a tool result mid-payload, modelling a tool that
// returns a partial response when its upstream connection drops.
//
// A resilient agent detects the payload is incomplete and retries or reports
// it, rather than treating the fragment as the full answer.
//
// The JSON-RPC envelope is preserved; only the result content is truncated.
type TruncationGremlin struct {
	probability float64
	maxCutRatio float64 // hard bound: never cut more than this fraction
	rng         *rand.Rand
}

// NewTruncationGremlin builds a TruncationGremlin. probability is the per-message
// injection chance; seed makes a session replayable.
func NewTruncationGremlin(probability, maxCutRatio float64, seed int64) *TruncationGremlin {
	return &TruncationGremlin{
		probability: probability,
		maxCutRatio: clamp(maxCutRatio, 0, 0.9),
		rng:         rand.New(rand.NewSource(seed)),
	}
}

// Name returns the gremlin's registry identifier.
func (g *TruncationGremlin) Name() string { return "truncation" }

// Inject truncates the result payload with probability g.probability.
// When it does not inject, msg is returned unmodified.
func (g *TruncationGremlin) Inject(ctx context.Context, msg protocol.Message) (protocol.Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return msg, false, err
	}
	// only tool results carry a payload worth truncating
	if !msg.IsToolResult() || g.rng.Float64() >= g.probability {
		return msg, false, nil // byte-exact no-op
	}

	payload := msg.ResultPayload()
	cut := int(float64(len(payload)) * (1 - g.rng.Float64()*g.maxCutRatio))
	out, err := msg.WithResultPayload(payload[:cut])
	if err != nil {
		return msg, false, fmt.Errorf("truncate result: %w", err)
	}
	return out, true, nil
}
```

## Tests — the bar

`gremlins_test.go`, table-driven:

- [ ] **Injected path**: the mutation happened, and it's the mutation you intended
- [ ] **Not-injected path**: `assert.Equal(t, in, out)` on the whole message — byte-exact
- [ ] Every message kind it may see, plus a kind it must **ignore** (returns `injected=false`)
- [ ] `ctx` cancelled mid-inject → returns the ctx error, doesn't mutate
- [ ] **Every bound at its boundary**: max size, max delay, max iterations
- [ ] **Determinism**: same seed + same input sequence → identical decisions, run twice

```go
func TestTruncationGremlin_Determinism(t *testing.T) {
	msgs := fixtureMessages(50)
	runA := collectDecisions(NewTruncationGremlin(0.5, 0.5, 42), msgs)
	runB := collectDecisions(NewTruncationGremlin(0.5, 0.5, 42), msgs)
	require.Equal(t, runA, runB, "same seed must produce identical injections")
}

func TestTruncationGremlin_NoOpIsExact(t *testing.T) {
	g := NewTruncationGremlin(0, 0.5, 1) // probability 0 → never injects
	in := fixtureToolResult("hello world")
	out, injected, err := g.Inject(context.Background(), in)
	require.NoError(t, err)
	require.False(t, injected)
	require.Equal(t, in, out, "not-injected path must be byte-exact")
}
```

- [ ] **Envelope test**: `id`, `jsonrpc`, `method` survive (unless a declared exception)
- [ ] `go test -race ./internal/arena/gremlins/ -count=2` clean

## Registry entry

```go
func init() {
	Register(Descriptor{
		Name:        "truncation",
		Title:       "TRUNCATION",
		Description: "Cuts a tool result mid-payload. Tests whether the agent notices an incomplete response instead of trusting the fragment.",
		Dimensions:  []scoring.Dimension{scoring.DataIntegrity},
		Knobs: []Knob{
			{Name: "probability", Type: "float", Default: 0.2, Min: 0, Max: 1},
			{Name: "max_cut_ratio", Type: "float", Default: 0.5, Min: 0, Max: 0.9},
		},
	})
}
```

The description carries personality **and** the mechanic — a user must finish reading it knowing what actually happens to their traffic. See `.claude/agents/design/whimsy-injector.md` for where the line is.

## If a new score dimension is needed

`internal/arena/scoring/dimensions.go`:
- [ ] Bounded **0–100**, monotone (more resilient → higher)
- [ ] Computable from recorded events alone
- [ ] **Every edge case defined** — empty session, all survived, all crashed, gremlin never fired. No `NaN`, no divide-by-zero
- [ ] Weight documented with the reason
- [ ] Tested for monotonicity in **both** directions (a fragile stream must score strictly lower)
- [ ] ⚠️ **A weight change breaks comparability with every historical score.** Flag it as breaking in the changelog

## Done means

```bash
go test -race -count=2 ./internal/arena/gremlins/ ./internal/arena/scoring/ ./internal/arena/session/
make check
go run ./cmd/arena          # :8082 — confirm the gremlin appears in GET /api/v1/gremlins
```

Then, end to end: launch a session from the dashboard with the new gremlin enabled, watch the live terminal, and confirm the resulting report attributes it to the right dimension.
