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

func TestGremlins_DefaultIsReproducible(t *testing.T) {
	first := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5), fixtureMessages(24))
	second := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5), fixtureMessages(24))
	assert.Equal(t, first, second,
		"a gremlin built without an explicit seed must still be reproducible")
}

func TestGremlins_StreamsAreIndependent(t *testing.T) {
	msgs := fixtureMessages(24)

	alone := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5, WithSeed(5)), msgs)

	other := NewLoopGremlin(3, "retry", 1.0, WithSeed(5))
	for _, m := range fixtureMessages(50) {
		_, _, _ = other.Inject(context.Background(), m)
	}
	alongside := trace(t, NewCorruptionGremlin(CorruptionModeMissingFields, 0.5, WithSeed(5)), msgs)

	assert.Equal(t, alone, alongside,
		"one gremlin's draws must not affect another's sequence")
}

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
