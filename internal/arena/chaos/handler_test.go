package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type fakeGremlin struct {
	name string
	fn   func(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error)
}

func (g *fakeGremlin) Name() string        { return g.name }
func (g *fakeGremlin) Description() string { return "fake gremlin for tests" }
func (g *fakeGremlin) Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	return g.fn(ctx, msg)
}

func alwaysInjects(name string) *fakeGremlin {
	return &fakeGremlin{
		name: name,
		fn: func(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
			out := cloneResponse(msg, fmt.Sprintf(`{"touched_by":%q}`, name))
			return out, true, nil
		},
	}
}

func corruptsResponsesOnly(name string) *fakeGremlin {
	return &fakeGremlin{
		name: name,
		fn: func(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
			if msg == nil || msg.Type != protocol.MessageTypeResponse {
				return msg, false, nil
			}
			return cloneResponse(msg, fmt.Sprintf(`{"touched_by":%q}`, name)), true, nil
		},
	}
}

func neverInjects(name string) *fakeGremlin {
	return &fakeGremlin{
		name: name,
		fn: func(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
			return msg, false, nil
		},
	}
}

func cloneResponse(msg *protocol.Message, result string) *protocol.Message {
	id, _ := msg.GetID()
	return &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      id,
			Result:  json.RawMessage(result),
		},
	}
}

func toolResult(id int64, body string) *protocol.Message {
	return &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(id),
			Result:  json.RawMessage(body),
		},
	}
}

func toolCall(id int64, method string) *protocol.Message {
	return &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(id),
			Method:  method,
		},
	}
}

func incoming() *proxy.MessageContext {
	return &proxy.MessageContext{
		ServerName:        "test",
		Direction:         models.DirectionIncoming,
		Timestamp:         time.Unix(0, 0),
		CorrelatedRequest: toolCall(1, "tools/call"),
	}
}

func newHandler(sink InjectionSink, gs ...gremlins.Gremlin) *GremlinHandler {
	n := 0
	return NewGremlinHandler(gs, sink, zerolog.Nop(),
		WithClock(func() time.Time { return time.Unix(1700000000, 0).UTC() }),
		WithIDGenerator(func() string { n++; return fmt.Sprintf("inj-%d", n) }),
	)
}

func TestGremlinHandler_ImplementsHandler(t *testing.T) {
	var h proxy.Handler = newHandler(nil)
	assert.Equal(t, "arena-gremlins", h.Name())
	assert.Equal(t, models.DirectionBoth, h.Direction())
}

func TestGremlinHandler_NoGremlinInjects_Skips(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, neverInjects("a"), neverInjects("b"))

	msg := toolResult(1, `{"ok":true}`)
	dec, err := h.HandleMessage(context.Background(), msg, incoming())

	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionSkip, dec.Action)
	assert.Nil(t, dec.ModifiedMessage)
	assert.Zero(t, log.Len(), "a declined injection must not be recorded")
}

func TestGremlinHandler_InjectsAndRecords(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, alwaysInjects("corruption"))

	msg := toolResult(42, `{"ok":true}`)
	dec, err := h.HandleMessage(context.Background(), msg, incoming())

	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionModify, dec.Action)
	require.NotNil(t, dec.ModifiedMessage)
	assert.Contains(t, string(dec.ModifiedMessage.Response.Result), "corruption")
	assert.Equal(t, "gremlin:corruption", dec.Reason)
	assert.Equal(t, "corruption", dec.Metadata["gremlin"])

	injections := log.Injections()
	require.Len(t, injections, 1)
	inj := injections[0]
	assert.Equal(t, "inj-1", inj.ID)
	assert.Equal(t, "corruption", inj.GremlinName)
	assert.Equal(t, models.DirectionIncoming, inj.Direction)
	assert.Equal(t, "42", inj.RequestID)
	assert.Equal(t, time.Unix(1700000000, 0).UTC(), inj.InjectedAt)
	assert.NotNil(t, inj.Original)
	assert.NotNil(t, inj.Modified)
}

func TestGremlinHandler_OnlyFirstGremlinInjects(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, alwaysInjects("first"), alwaysInjects("second"))

	dec, err := h.HandleMessage(context.Background(), toolResult(1, `{}`), incoming())

	require.NoError(t, err)
	assert.Equal(t, "gremlin:first", dec.Reason)
	assert.Contains(t, string(dec.ModifiedMessage.Response.Result), "first")
	assert.NotContains(t, string(dec.ModifiedMessage.Response.Result), "second")
	assert.Equal(t, 1, log.Len(), "exactly one injection per message")
}

func TestGremlinHandler_SkipsPastDecliningGremlins(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, neverInjects("quiet"), alwaysInjects("loud"))

	dec, err := h.HandleMessage(context.Background(), toolResult(1, `{}`), incoming())

	require.NoError(t, err)
	assert.Equal(t, "gremlin:loud", dec.Reason)
	assert.Equal(t, 1, log.Len())
}

func TestGremlinHandler_GremlinErrorPassesMessageThrough(t *testing.T) {
	log := NewInjectionLog()
	boom := &fakeGremlin{
		name: "boom",
		fn: func(_ context.Context, _ *protocol.Message) (*protocol.Message, bool, error) {
			return nil, false, errors.New("gremlin exploded")
		},
	}
	h := newHandler(log, boom)

	dec, err := h.HandleMessage(context.Background(), toolResult(1, `{}`), incoming())

	require.NoError(t, err, "a gremlin failure must not surface as a pipeline error")
	assert.Equal(t, proxy.DecisionSkip, dec.Action)
	assert.Zero(t, log.Len())
}

func TestGremlinHandler_GremlinErrorFallsThroughToNext(t *testing.T) {
	log := NewInjectionLog()
	boom := &fakeGremlin{
		name: "boom",
		fn: func(_ context.Context, _ *protocol.Message) (*protocol.Message, bool, error) {
			return nil, false, errors.New("gremlin exploded")
		},
	}
	h := newHandler(log, boom, alwaysInjects("healthy"))

	dec, err := h.HandleMessage(context.Background(), toolResult(1, `{}`), incoming())

	require.NoError(t, err)
	assert.Equal(t, "gremlin:healthy", dec.Reason)
}

func TestGremlinHandler_InjectedButNilMessageIsIgnored(t *testing.T) {
	log := NewInjectionLog()
	liar := &fakeGremlin{
		name: "liar",
		fn: func(_ context.Context, _ *protocol.Message) (*protocol.Message, bool, error) {
			return nil, true, nil
		},
	}
	h := newHandler(log, liar)

	dec, err := h.HandleMessage(context.Background(), toolResult(1, `{}`), incoming())

	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionSkip, dec.Action)
	assert.Zero(t, log.Len(), "a contract violation must not be recorded as an injection")
}

func TestGremlinHandler_CancelledContextSkips(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, alwaysInjects("corruption"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dec, err := h.HandleMessage(ctx, toolResult(1, `{}`), incoming())

	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionSkip, dec.Action)
	assert.Zero(t, log.Len())
}

func TestGremlinHandler_NilSinkIsSafe(t *testing.T) {
	h := newHandler(nil, alwaysInjects("corruption"))

	dec, err := h.HandleMessage(context.Background(), toolResult(1, `{}`), incoming())

	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionModify, dec.Action)
}

func TestGremlinHandler_InjectionOrderIsStable(t *testing.T) {
	run := func() []string {
		log := NewInjectionLog()
		h := newHandler(log,
			neverInjects("a"),
			alwaysInjects("b"),
			alwaysInjects("c"),
		)
		for i := int64(1); i <= 5; i++ {
			_, err := h.HandleMessage(context.Background(), toolResult(i, `{}`), incoming())
			require.NoError(t, err)
		}
		names := make([]string, 0, log.Len())
		for _, inj := range log.Injections() {
			names = append(names, inj.GremlinName)
		}
		return names
	}

	first := run()
	second := run()
	assert.Equal(t, first, second, "same order in, same injections out")
	assert.Equal(t, []string{"b", "b", "b", "b", "b"}, first,
		"the first injecting gremlin in the slice must win every time")
}

func TestGremlinHandler_MethodFromRequest(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, &fakeGremlin{
		name: "hallucination",
		fn: func(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
			return msg, true, nil
		},
	})
	mctx := &proxy.MessageContext{ServerName: "s", Direction: models.DirectionOutgoing}
	_, err := h.HandleMessage(context.Background(), toolCall(3, "tools/call"), mctx)
	require.NoError(t, err)

	require.Len(t, log.Injections(), 1)
	assert.Equal(t, "tools/call", log.Injections()[0].Method)
}

func TestGremlinHandler_MethodFromCorrelatedRequest(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, alwaysInjects("corruption"))

	mctx := incoming()
	mctx.CorrelatedRequest = toolCall(7, "tools/call")

	_, err := h.HandleMessage(context.Background(), toolResult(7, `{}`), mctx)
	require.NoError(t, err)

	require.Len(t, log.Injections(), 1)
	assert.Equal(t, "tools/call", log.Injections()[0].Method,
		"a response's method must come from the request it answers")
}

func TestGremlinHandler_OnlyToolCallsAreEligible(t *testing.T) {
	for _, method := range []string{"server/discover", "initialize", "tools/list",
		"resources/list", "resources/templates/list", "ping", "notifications/progress"} {
		log := NewInjectionLog()
		h := newHandler(log, alwaysInjects("noop"))
		notif := &protocol.Message{
			Type: protocol.MessageTypeNotification,
			Notification: &protocol.JSONRPCNotification{
				JSONRPC: protocol.JSONRPCVersion,
				Method:  method,
			},
		}
		dec, err := h.HandleMessage(context.Background(), notif, outgoing())
		require.NoError(t, err)
		assert.Equal(t, proxy.DecisionSkip, dec.Action, method)
		assert.Empty(t, log.Injections(), "%s is not agent work and must not be injected", method)
	}
}

func TestGremlinHandler_MutatesThroughRealPipeline(t *testing.T) {
	log := NewInjectionLog()
	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(newHandler(log, alwaysInjects("corruption")))

	original := toolResult(1, `{"pristine":true}`)
	dec, forwarded, err := pipeline.Process(context.Background(), original, incoming())

	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionModify, dec.Action)
	require.NotNil(t, forwarded)
	assert.Contains(t, string(forwarded.Response.Result), "corruption",
		"the pipeline must forward the gremlin's mutation")
	assert.NotContains(t, string(forwarded.Response.Result), "pristine")
	assert.Equal(t, 1, log.Len())
}

func TestGremlinHandler_PipelineSuppliesCorrelation(t *testing.T) {
	log := NewInjectionLog()
	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(newHandler(log, alwaysInjects("corruption")))

	ctx := context.Background()
	out := &proxy.MessageContext{ServerName: "s", Direction: models.DirectionOutgoing}
	_, _, err := pipeline.Process(ctx, toolCall(9, "tools/call"), out)
	require.NoError(t, err)

	_, _, err = pipeline.Process(ctx, toolResult(9, `{}`), incoming())
	require.NoError(t, err)

	injections := log.Injections()
	require.NotEmpty(t, injections)
	last := injections[len(injections)-1]
	assert.Equal(t, "tools/call", last.Method,
		"the pipeline's correlator must let the handler attribute a response")
}

func TestGremlinHandler_BlockedBeforeChaosRuns(t *testing.T) {
	log := NewInjectionLog()
	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(&blockingHandler{})
	pipeline.RegisterHandler(newHandler(log, alwaysInjects("corruption")))

	dec, forwarded, err := pipeline.Process(context.Background(),
		toolCall(1, "tools/call"),
		&proxy.MessageContext{ServerName: "s", Direction: models.DirectionOutgoing})
	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionBlock, dec.Action)
	assert.Nil(t, forwarded)
	assert.Zero(t, log.Len(), "chaos must not run on a message policy already blocked")
}

type blockingHandler struct{}

func (b *blockingHandler) Name() string                { return "test-blocker" }
func (b *blockingHandler) Priority() int               { return 10 }
func (b *blockingHandler) Direction() models.Direction { return models.DirectionBoth }
func (b *blockingHandler) HandleMessage(context.Context, *protocol.Message, *proxy.MessageContext) (*proxy.Decision, error) {
	return &proxy.Decision{Action: proxy.DecisionBlock, Reason: "policy"}, nil
}

func TestGremlinHandler_MutatesRealWrapTraffic(t *testing.T) {
	log := NewInjectionLog()
	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(newHandler(log, alwaysInjects("corruption")))

	const script = `while IFS= read -r line; do
	  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
	  [ -n "$id" ] && printf '{"jsonrpc":"2.0","id":%s,"result":{"pristine":true}}\n' "$id"
	done`

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read_graph"}}` + "\n")
	out := &syncBuffer{}
	p := proxy.NewWrapProxy(proxy.Config{
		ServerName: "fake-mcp",
		Command:    "sh",
		Args:       []string{"-c", script},
	},
		proxy.WithLogger(zerolog.Nop()),
		proxy.WithPipeline(pipeline),
		proxy.WithClientIO(in, out),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, p.Start(ctx))

	got := out.String()
	require.NotEmpty(t, got, "the client received nothing")
	assert.Contains(t, got, "corruption", "the gremlin's mutation must reach the client")
	assert.NotContains(t, got, "pristine", "the server's original answer must have been replaced")
	assert.Positive(t, log.Len(), "the injection must have been recorded")
}
