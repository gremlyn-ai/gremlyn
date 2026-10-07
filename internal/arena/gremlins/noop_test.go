package gremlins

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allGremlins(t *testing.T) map[string]Gremlin {
	t.Helper()
	opt := WithSeed(1)
	return map[string]Gremlin{
		"hallucination": NewHallucinationGremlin("nonexistent_tool", 0, opt),
		"latency":       NewLatencyGremlin(1, 2, 0, opt),
		"corruption":    NewCorruptionGremlin(CorruptionModeMissingFields, 0, opt),
		"loop":          NewLoopGremlin(3, "retry", 0, opt),
		"overflow":      NewOverflowGremlin(1024, 0, opt),
		"timeout":       NewTimeoutGremlin(1, 2, 0, opt),
	}
}

func everyMessageKind() map[string]*protocol.Message {
	return map[string]*protocol.Message{
		"tools/call request": {
			Type: protocol.MessageTypeRequest,
			Request: &protocol.JSONRPCRequest{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      protocol.NewIntID(1),
				Method:  string(protocol.MCPMethodToolsCall),
				Params:  json.RawMessage(`{"name":"search","arguments":{"q":"x"}}`),
			},
		},
		"tool result response": {
			Type: protocol.MessageTypeResponse,
			Response: &protocol.JSONRPCResponse{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      protocol.NewIntID(1),
				Result:  json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`),
			},
		},
		"error response": {
			Type: protocol.MessageTypeResponse,
			Response: &protocol.JSONRPCResponse{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      protocol.NewIntID(1),
				Error:   &protocol.JSONRPCError{Code: -32000, Message: "boom"},
			},
		},
		"notification": {
			Type: protocol.MessageTypeNotification,
			Notification: &protocol.JSONRPCNotification{
				JSONRPC: protocol.JSONRPCVersion,
				Method:  "notifications/initialized",
			},
		},
		"initialize request": {
			Type: protocol.MessageTypeRequest,
			Request: &protocol.JSONRPCRequest{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      protocol.NewIntID(1),
				Method:  "initialize",
				Params:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
			},
		},
	}
}

func mustMarshal(t *testing.T, msg *protocol.Message) string {
	t.Helper()
	b, err := json.Marshal(msg)
	require.NoError(t, err)
	return string(b)
}

func TestGremlins_NotInjectedIsByteExactNoOp(t *testing.T) {
	for name, g := range allGremlins(t) {
		for kind, msg := range everyMessageKind() {
			t.Run(name+"/"+kind, func(t *testing.T) {
				before := mustMarshal(t, msg)

				out, injected, err := g.Inject(context.Background(), msg)
				require.NoError(t, err)
				require.False(t, injected,
					"probability is 0, so this gremlin must decline")

				require.NotNil(t, out, "a declining gremlin must still return the message")
				assert.Equal(t, before, mustMarshal(t, out),
					"declining changed the message — a control run is contaminated and the session is no longer replayable")

				assert.Equal(t, before, mustMarshal(t, msg),
					"the gremlin mutated its input")
			})
		}
	}
}

func TestGremlins_NilMessageIsSafe(t *testing.T) {
	for name, g := range allGremlins(t) {
		t.Run(name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				_, injected, _ := g.Inject(context.Background(), nil)
				assert.False(t, injected, "there is nothing to inject into")
			})
		})
	}
}

func TestGremlins_CancelledContextReturnsPromptly(t *testing.T) {
	opt := WithSeed(1)
	blocking := map[string]Gremlin{
		"latency": NewLatencyGremlin(30_000, 60_000, 1.0, opt),
		"timeout": NewTimeoutGremlin(30_000, 60_000, 1.0, opt),
	}

	msg := everyMessageKind()["tool result response"]

	for name, g := range blocking {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			done := make(chan struct{})
			go func() {
				defer close(done)
				_, _, _ = g.Inject(ctx, msg)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("gremlin ignored a cancelled context and kept blocking")
			}
		})
	}
}

func TestGremlins_DeterministicUnderASeed(t *testing.T) {
	msg := everyMessageKind()["tool result response"]

	run := func() []string {
		out := make([]string, 0, 3*20)

		for name, g := range map[string]Gremlin{
			"corruption": NewCorruptionGremlin(CorruptionModeMissingFields, 0.5, WithSeed(42)),
			"loop":       NewLoopGremlin(3, "retry", 0.5, WithSeed(42)),
			"overflow":   NewOverflowGremlin(1024, 0.5, WithSeed(42)),
		} {
			for range 20 {
				_, injected, err := g.Inject(context.Background(), msg)
				require.NoError(t, err)
				out = append(out, name+":"+boolStr(injected))
			}
		}
		return out
	}

	first, second := run(), run()
	assert.ElementsMatch(t, first, second,
		"the same seed produced different injection decisions")
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
