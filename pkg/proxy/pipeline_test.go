package proxy

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testHandler struct {
	name      string
	priority  int
	direction models.Direction
	decision  *Decision
	called    atomic.Bool
}

func (h *testHandler) Name() string                { return h.name }
func (h *testHandler) Priority() int               { return h.priority }
func (h *testHandler) Direction() models.Direction { return h.direction }
func (h *testHandler) HandleMessage(_ context.Context, _ *protocol.Message, _ *MessageContext) (*Decision, error) {
	h.called.Store(true)
	return h.decision, nil
}

func newTestMsg() *protocol.Message {
	return &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      protocol.NewIntID(1),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"test"}`),
		},
	}
}

func newTestMCtx() *MessageContext {
	return &MessageContext{
		ServerName: "test-server",
		Direction:  models.DirectionOutgoing,
		Timestamp:  time.Now(),
	}
}

func TestPipeline_PriorityOrder(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	h1 := &testHandler{
		name: "second", priority: 200, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionAllow},
	}
	h2 := &testHandler{
		name: "first", priority: 100, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionAllow},
	}

	pipeline.RegisterHandler(h1)
	pipeline.RegisterHandler(h2)

	pipeline.mu.RLock()
	callOrder := make([]string, 0, len(pipeline.handlers))
	for _, h := range pipeline.handlers {
		callOrder = append(callOrder, h.Name())
	}
	pipeline.mu.RUnlock()
	assert.Equal(t, []string{"first", "second"}, callOrder)
}

func TestPipeline_BlockShortCircuits(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	blocker := &testHandler{
		name: "blocker", priority: 100, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionBlock, Reason: "blocked by policy"},
	}
	afterBlocker := &testHandler{
		name: "after", priority: 200, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionAllow},
	}

	pipeline.RegisterHandler(blocker)
	pipeline.RegisterHandler(afterBlocker)

	decision, msg, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.Equal(t, DecisionBlock, decision.Action)
	assert.Nil(t, msg)
	assert.True(t, blocker.called.Load())
	assert.False(t, afterBlocker.called.Load())
}

func TestPipeline_ModifyChains(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	modifiedMsg := &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      protocol.NewIntID(1),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"modified"}`),
		},
	}

	modifier := &testHandler{
		name: "modifier", priority: 100, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionModify, ModifiedMessage: modifiedMsg},
	}
	pipeline.RegisterHandler(modifier)

	decision, msg, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.Equal(t, DecisionModify, decision.Action)
	assert.NotNil(t, msg)
	assert.Equal(t, `{"name":"modified"}`, string(msg.Request.Params))
}

func TestPipeline_DirectionFiltering(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	incomingOnly := &testHandler{
		name: "incoming-only", priority: 100, direction: models.DirectionIncoming,
		decision: &Decision{Action: DecisionBlock, Reason: "should not be called"},
	}
	pipeline.RegisterHandler(incomingOnly)

	decision, _, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.Equal(t, DecisionAllow, decision.Action)
	assert.False(t, incomingOnly.called.Load())
}

func TestPipeline_RequestCorrelation(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	var capturedCorrelated *protocol.Message

	correlationChecker := &testHandler{
		name: "checker", priority: 100, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionAllow},
	}

	reqMsg := newTestMsg()
	outCtx := &MessageContext{
		ServerName: "test", Direction: models.DirectionOutgoing, Timestamp: time.Now(),
	}

	pipeline.RegisterHandler(correlationChecker)
	_, _, _ = pipeline.Process(context.Background(), reqMsg, outCtx)

	respMsg := &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      protocol.NewIntID(1),
			Result:  json.RawMessage(`{}`),
		},
	}
	inCtx := &MessageContext{
		ServerName: "test", Direction: models.DirectionIncoming, Timestamp: time.Now(),
	}
	_, _, _ = pipeline.Process(context.Background(), respMsg, inCtx)
	capturedCorrelated = inCtx.CorrelatedRequest

	assert.NotNil(t, capturedCorrelated)
	assert.Equal(t, protocol.MessageTypeRequest, capturedCorrelated.Type)
	assert.Equal(t, "tools/call", capturedCorrelated.Request.Method)
}

func TestPipeline_DefaultAllow(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	decision, msg, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.Equal(t, DecisionAllow, decision.Action)
	assert.NotNil(t, msg)
}

func TestNewBlockErrorResponse(t *testing.T) {
	msg := NewBlockErrorResponse(protocol.NewIntID(42), "policy violation")
	assert.Equal(t, protocol.MessageTypeResponse, msg.Type)
	assert.NotNil(t, msg.Response.Error)
	assert.Equal(t, -32600, msg.Response.Error.Code)
	assert.Contains(t, msg.Response.Error.Message, "policy violation")
	assert.Equal(t, int64(42), msg.Response.ID.IntVal)
}

func TestRequestCorrelator_TrackAndCorrelate(t *testing.T) {
	rc := NewRequestCorrelator(5 * time.Minute)

	reqMsg := newTestMsg()
	rc.TrackRequest(reqMsg)

	respMsg := &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: "2.0", ID: protocol.NewIntID(1), Result: json.RawMessage(`{}`),
		},
	}

	correlated := rc.CorrelateResponse(respMsg)
	assert.NotNil(t, correlated)
	assert.Equal(t, "tools/call", correlated.Request.Method)

	correlated = rc.CorrelateResponse(respMsg)
	assert.Nil(t, correlated)
}

func TestRequestCorrelator_NoMatch(t *testing.T) {
	rc := NewRequestCorrelator(5 * time.Minute)

	respMsg := &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: "2.0", ID: protocol.NewIntID(999), Result: json.RawMessage(`{}`),
		},
	}

	correlated := rc.CorrelateResponse(respMsg)
	assert.Nil(t, correlated)
}

func TestPipeline_RegisterAndProcess(t *testing.T) {
	pipeline := NewPipeline(zerolog.Nop())
	t.Cleanup(pipeline.Close)

	handler := &testHandler{
		name:      "test",
		priority:  100,
		direction: models.DirectionBoth,
		decision:  &Decision{Action: DecisionAllow},
	}
	pipeline.RegisterHandler(handler)

	decision, msg, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.True(t, handler.called.Load())
	assert.Equal(t, DecisionAllow, decision.Action)
	assert.NotNil(t, msg)
}

type panickingHandler struct{ name string }

func (h *panickingHandler) Name() string                { return h.name }
func (h *panickingHandler) Priority() int               { return 10 }
func (h *panickingHandler) Direction() models.Direction { return models.DirectionBoth }
func (h *panickingHandler) HandleMessage(context.Context, *protocol.Message, *MessageContext) (*Decision, error) {
	panic("handler exploded")
}

type blockingHandler struct{ name string }

func (h *blockingHandler) Name() string                { return h.name }
func (h *blockingHandler) Priority() int               { return 20 }
func (h *blockingHandler) Direction() models.Direction { return models.DirectionBoth }
func (h *blockingHandler) HandleMessage(context.Context, *protocol.Message, *MessageContext) (*Decision, error) {
	return &Decision{Action: DecisionBlock, Reason: "blocked by the later stage", RuleID: h.name}, nil
}

func aRequest() *protocol.Message {
	return &protocol.Message{
		Type: MessageTypeRequestForTest(),
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(1),
			Method:  string(protocol.MCPMethodToolsCall),
			Params:  []byte(`{"name":"t","arguments":{}}`),
		},
	}
}
func MessageTypeRequestForTest() protocol.MessageType { return protocol.MessageTypeRequest }
func TestPipeline_ContainsAHandlerPanic(t *testing.T) {
	p := NewPipeline(zerolog.Nop())
	t.Cleanup(p.Close)
	p.RegisterHandler(&panickingHandler{name: "boom"})
	require.NotPanics(t, func() {
		dec, out, err := p.Process(context.Background(), aRequest(),
			&MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
		require.NoError(t, err, "a contained panic must not surface as a pipeline error")
		require.NotNil(t, dec)
		assert.Equal(t, DecisionAllow, dec.Action,
			"the panicking stage is skipped, so nothing decided otherwise")
		assert.NotNil(t, out)
	})
}

func TestPipeline_PanicDoesNotSkipLaterHandlers(t *testing.T) {
	p := NewPipeline(zerolog.Nop())
	t.Cleanup(p.Close)
	p.RegisterHandler(&panickingHandler{name: "boom"})
	p.RegisterHandler(&blockingHandler{name: "still-enforcing"})
	dec, out, err := p.Process(context.Background(), aRequest(),
		&MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
	require.NoError(t, err)

	require.Equal(t, DecisionBlock, dec.Action,
		"a panic in an earlier stage must not disable the ones after it")
	assert.Equal(t, "still-enforcing", dec.RuleID)
	assert.Nil(t, out)
}

func TestPipeline_SurvivesRepeatedPanics(t *testing.T) {
	p := NewPipeline(zerolog.Nop())
	t.Cleanup(p.Close)
	p.RegisterHandler(&panickingHandler{name: "boom"})
	for range 50 {
		_, _, err := p.Process(context.Background(), aRequest(),
			&MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
		require.NoError(t, err)
	}
}

func TestPipeline_CloseIsIdempotent(t *testing.T) {
	p := NewPipeline(zerolog.Nop())
	require.NotPanics(t, func() {
		p.Close()
		p.Close()
	})
}
