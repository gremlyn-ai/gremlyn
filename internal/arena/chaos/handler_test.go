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

// syncBuffer is an io.Writer safe for concurrent use, since the proxy writes to
// the client stream from its own goroutine while the test reads it.
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

// fakeGremlin is a Gremlin whose behaviour the test dictates.
type fakeGremlin struct {
	name string
	fn   func(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error)
}

func (g *fakeGremlin) Name() string        { return g.name }
func (g *fakeGremlin) Description() string { return "fake gremlin for tests" }
func (g *fakeGremlin) Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	return g.fn(ctx, msg)
}

// alwaysInjects returns a gremlin that rewrites the result payload every time.
func alwaysInjects(name string) *fakeGremlin {
	return &fakeGremlin{
		name: name,
		fn: func(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
			out := cloneResponse(msg, fmt.Sprintf(`{"touched_by":%q}`, name))
			return out, true, nil
		},
	}
}

// corruptsResponsesOnly is how a real result-corrupting gremlin behaves: it
// rewrites incoming tool results and leaves the agent's own outgoing requests
// alone.
//
// alwaysInjects is deliberately cruder and rewrites anything, which is fine for
// unit-testing the handler but wrong for anything that then observes the agent —
// mutating the agent's request out of shape makes its reaction invisible.
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

// neverInjects returns a gremlin that always declines, returning the message
// byte-identical as the contract requires.
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
		ServerName: "test",
		Direction:  models.DirectionIncoming,
		Timestamp:  time.Unix(0, 0),
	}
}

func newHandler(sink InjectionSink, gs ...gremlins.Gremlin) *GremlinHandler {
	n := 0
	return NewGremlinHandler(gs, sink, zerolog.Nop(),
		WithClock(func() time.Time { return time.Unix(1700000000, 0).UTC() }),
		WithIDGenerator(func() string { n++; return fmt.Sprintf("inj-%d", n) }),
	)
}

// ── Pipeline contract ──

func TestGremlinHandler_ImplementsHandler(t *testing.T) {
	var h proxy.Handler = newHandler(nil)
	assert.Equal(t, "arena-gremlins", h.Name())
	assert.Equal(t, models.DirectionBoth, h.Direction())
}

// Chaos must run after Shield's policy engine (priority 10) so a gremlin can
// never smuggle a payload past a decision that would have blocked it.
func TestGremlinHandler_RunsAfterShieldPolicy(t *testing.T) {
	const shieldPolicyPriority = 10
	assert.Greater(t, newHandler(nil).Priority(), shieldPolicyPriority)
}

// ── Injection behaviour ──

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

// Only one gremlin may fire per message: compounding mutations would make the
// agent's reaction impossible to attribute to a cause.
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

// ── Failure containment ──

// Arena is a testing tool. If a gremlin's own bug breaks the user's traffic,
// every result the session produces afterwards is noise.
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

// A gremlin claiming an injection while returning no message would drop the
// message and hang the agent, which looks like a gremlin working when it is not.
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

// ── Determinism ──

// A session whose injection order varies between runs cannot be replayed, and a
// score that is not reproducible is not a score. Registry.List() iterates a map,
// so the handler takes an ordered slice — this pins that.
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

// ── Correlation metadata ──

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

// A response carries no method of its own. Without the correlated request, an
// injection on a tool result could not be attributed to the tool that was called.
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

func TestGremlinHandler_NotificationHasNoRequestID(t *testing.T) {
	log := NewInjectionLog()
	h := newHandler(log, &fakeGremlin{
		name: "noop",
		fn: func(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
			return msg, true, nil
		},
	})

	notif := &protocol.Message{
		Type: protocol.MessageTypeNotification,
		Notification: &protocol.JSONRPCNotification{
			JSONRPC: protocol.JSONRPCVersion,
			Method:  "notifications/initialized",
		},
	}
	_, err := h.HandleMessage(context.Background(), notif, incoming())
	require.NoError(t, err)

	require.Len(t, log.Injections(), 1)
	assert.Empty(t, log.Injections()[0].RequestID)
	assert.Equal(t, "notifications/initialized", log.Injections()[0].Method)
}

// ── Through a real pipeline ──

// This is the point of P0.1: gremlins mutate messages travelling through the
// shared proxy pipeline, not fabricated ones.
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

// The pipeline correlates a response to its request. Registering only the gremlin
// handler, an injection on the response must still carry the request's method.
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

// A Shield-style blocking handler at a lower priority must short-circuit before
// chaos runs, so a gremlin cannot resurrect a blocked message.
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

// ── Through a real wrap proxy ──

// The end-to-end claim: chaos reaches an actual MCP conversation. The fake server
// answers every request; the gremlin rewrites the answer on its way back, and the
// client sees the mutation.
func TestGremlinHandler_MutatesRealWrapTraffic(t *testing.T) {
	log := NewInjectionLog()
	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(newHandler(log, alwaysInjects("corruption")))

	const script = `while IFS= read -r line; do
	  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
	  [ -n "$id" ] && printf '{"jsonrpc":"2.0","id":%s,"result":{"pristine":true}}\n' "$id"
	done`

	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	out := &syncBuffer{}

	p := proxy.NewWrapProxy(proxy.Config{
		ServerName: "fake-mcp",
		Mode:       models.ServerModeWrap,
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
