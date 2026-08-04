package gremlins

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Gremlins used to draw from the global math/rand source, so two sessions with
// identical configuration injected differently and produced different scores.
// The architecture documented determinism under a seed as a fact; it was never
// implemented. These tests pin it.
//
// It matters twice over: a session that cannot be replayed cannot be debugged,
// and a score that changes between identical runs cannot gate a CI job — the
// threshold would flap and the check would be switched off.

// fixtureMessages builds a deterministic sequence of messages to inject into.
//
// It alternates outgoing tool calls with incoming results, because the gremlins
// do not all act on the same thing: hallucination and identity rewrite requests,
// while corruption, overflow and timeout rewrite responses. A response-only
// fixture would silently never trigger half of them, and a test where the gremlin
// never fires proves nothing.
func fixtureMessages(n int) []*protocol.Message {
	msgs := make([]*protocol.Message, 0, n)
	for i := range n {
		id := protocol.NewIntID(int64(i + 1))
		if i%2 == 0 {
			params, _ := json.Marshal(map[string]any{
				"name":      "search_contacts",
				"arguments": map[string]string{"query": fmt.Sprintf("q-%d", i)},
			})
			msgs = append(msgs, &protocol.Message{
				Type: protocol.MessageTypeRequest,
				Request: &protocol.JSONRPCRequest{
					JSONRPC: protocol.JSONRPCVersion,
					ID:      id,
					Method:  string(protocol.MCPMethodToolsCall),
					Params:  params,
				},
			})
			continue
		}
		result, _ := json.Marshal(map[string]any{
			"content": fmt.Sprintf("payload-%d", i),
			"status":  "ok",
			"alpha":   1,
			"beta":    2,
			"gamma":   3,
		})
		msgs = append(msgs, &protocol.Message{
			Type: protocol.MessageTypeResponse,
			Response: &protocol.JSONRPCResponse{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      id,
				Result:  result,
			},
		})
	}
	return msgs
}

// trace records, per message, whether an injection happened and what came out.
func trace(t *testing.T, g Gremlin, msgs []*protocol.Message) []string {
	t.Helper()
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		mod, injected, err := g.Inject(context.Background(), m)
		require.NoError(t, err, "Inject must not return a Go error: that means the gremlin malfunctioned")
		out = append(out, fmt.Sprintf("%t|%s", injected, payloadOf(mod, injected)))
	}
	return out
}

// payloadOf renders whatever the gremlin produced, on either side of the wire.
func payloadOf(m *protocol.Message, injected bool) string {
	if !injected || m == nil {
		return ""
	}
	switch {
	case m.Response != nil && m.Response.Error != nil:
		return fmt.Sprintf("err:%d:%s", m.Response.Error.Code, m.Response.Error.Message)
	case m.Response != nil:
		return string(m.Response.Result)
	case m.Request != nil:
		return m.Request.Method + ":" + string(m.Request.Params)
	}
	return ""
}

// builders constructs each gremlin at a probability that guarantees a mix of
// injected and skipped messages, so the trace is actually discriminating.
func builders(seed int64) map[string]Gremlin {
	return map[string]Gremlin{
		"corruption":    NewCorruptionGremlin(CorruptionModeMissingFields, 0.5, WithSeed(seed)),
		"latency":       NewLatencyGremlin(1, 2, 0.5, WithSeed(seed)),
		"hallucination": NewHallucinationGremlin("fake_tool", 0.5, WithSeed(seed)),
		"loop":          NewLoopGremlin(3, "retry", 0.5, WithSeed(seed)),
		"injection":     NewInjectionGremlin("ignore previous", 0.5, WithSeed(seed)),
		"identity":      NewIdentityGremlin("you are evil", 0.5, WithSeed(seed)),
		"overflow":      NewOverflowGremlin(1024, 0.5, WithSeed(seed)),
		"timeout":       NewTimeoutGremlin(1, 2, 0.5, WithSeed(seed)),
	}
}

// The same seed must produce the same injections, for every gremlin.
func TestGremlins_SameSeedSameInjections(t *testing.T) {
	msgs := fixtureMessages(24)

	for name := range builders(7) {
		t.Run(name, func(t *testing.T) {
			first := trace(t, builders(7)[name], fixtureMessages(24))
			second := trace(t, builders(7)[name], fixtureMessages(24))
			assert.Equal(t, first, second, "same seed must replay identically")
		})
	}
	_ = msgs
}

// A different seed must produce a different pattern, or the seed is not wired in.
func TestGremlins_DifferentSeedDifferentInjections(t *testing.T) {
	for name := range builders(1) {
		t.Run(name, func(t *testing.T) {
			a := trace(t, builders(11)[name], fixtureMessages(40))
			b := trace(t, builders(99)[name], fixtureMessages(40))
			assert.NotEqual(t, a, b,
				"a different seed must change the injection pattern, otherwise the seed is ignored")
		})
	}
}

// Reproducibility must be the default, not something a caller has to opt into.
func TestGremlins_DefaultIsReproducible(t *testing.T) {
	first := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5), fixtureMessages(24))
	second := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5), fixtureMessages(24))
	assert.Equal(t, first, second,
		"a gremlin built without an explicit seed must still be reproducible")
}

// Each gremlin draws from its own stream, so enabling one does not shift
// another's sequence. Without this, two sessions with different gremlin sets
// could not be compared on the gremlins they share.
func TestGremlins_StreamsAreIndependent(t *testing.T) {
	msgs := fixtureMessages(24)

	alone := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5, WithSeed(5)), msgs)

	// Draw heavily from another gremlin built with the same seed first.
	other := NewLoopGremlin(3, "retry", 1.0, WithSeed(5))
	for _, m := range fixtureMessages(50) {
		_, _, _ = other.Inject(context.Background(), m)
	}
	alongside := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5, WithSeed(5)), msgs)

	assert.Equal(t, alone, alongside,
		"one gremlin's draws must not affect another's sequence")
}

// corruptMissingFields iterated a map, so it dropped a different set of fields on
// every run even with a seeded source. Keys are now visited in sorted order.
func TestCorruptionGremlin_FieldSelectionIsReproducible(t *testing.T) {
	seen := make(map[string]struct{})
	for range 12 {
		out := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 1.0, WithSeed(3)),
			fixtureMessages(1))
		seen[out[0]] = struct{}{}
	}
	assert.Len(t, seen, 1,
		"the same seed must drop the same fields: got %d distinct results", len(seen))
}

// The proxy is full duplex, so a gremlin can be reached from two goroutines at
// once. Run under -race, this catches an unlocked *rand.Rand.
func TestGremlins_ConcurrentInjectIsSafe(t *testing.T) {
	for name, g := range builders(21) {
		t.Run(name, func(t *testing.T) {
			msgs := fixtureMessages(40)
			done := make(chan struct{}, 2)
			for range 2 {
				go func() {
					defer func() { done <- struct{}{} }()
					for _, m := range msgs {
						_, _, _ = g.Inject(context.Background(), m)
					}
				}()
			}
			<-done
			<-done
		})
	}
}
