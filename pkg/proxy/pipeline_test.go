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

// --- Test handler implementations ---

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

type testAsyncHandler struct {
	testHandler
	asyncCalled  atomic.Bool
	lastDecision atomic.Value
}

func (h *testAsyncHandler) HandleAsync(_ context.Context, _ *protocol.Message, _ *MessageContext, decision *Decision) {
	h.asyncCalled.Store(true)
	h.lastDecision.Store(decision)
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

func TestPipeline_RegisterAndProcess(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	handler := &testHandler{
		name:      "test",
		priority:  100,
		direction: models.DirectionBoth,
		decision:  &Decision{Action: DecisionAllow},
	}
	pipeline.RegisterHandler(handler)

	assert.Equal(t, 1, pipeline.HandlerCount())

	decision, msg, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.True(t, handler.called.Load())
	assert.Equal(t, DecisionAllow, decision.Action)
	assert.NotNil(t, msg)
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

	// Verify the pipeline's internal ordering.
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
	assert.Nil(t, msg) // Block returns nil message.
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

	// Process an outgoing message — the incoming-only handler should be skipped.
	decision, _, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.Equal(t, DecisionAllow, decision.Action) // Default allow.
	assert.False(t, incomingOnly.called.Load())
}

func TestPipeline_AsyncHandler(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	asyncH := &testAsyncHandler{
		testHandler: testHandler{
			name: "async-logger", priority: 100, direction: models.DirectionBoth,
			decision: &Decision{Action: DecisionSkip},
		},
	}
	pipeline.RegisterAsyncHandler(asyncH)

	_, _, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)

	// Give the goroutine a moment to run.
	time.Sleep(50 * time.Millisecond)
	assert.True(t, asyncH.asyncCalled.Load())
}

func TestPipeline_UnregisterHandler(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	handler := &testHandler{
		name: "removable", priority: 100, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionBlock},
	}
	pipeline.RegisterHandler(handler)
	assert.Equal(t, 1, pipeline.HandlerCount())

	pipeline.UnregisterHandler("removable")
	assert.Equal(t, 0, pipeline.HandlerCount())

	// Process should now allow (no handlers).
	decision, _, err := pipeline.Process(context.Background(), newTestMsg(), newTestMCtx())
	require.NoError(t, err)
	assert.Equal(t, DecisionAllow, decision.Action)
}

func TestPipeline_RequestCorrelation(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	var capturedCorrelated *protocol.Message

	correlationChecker := &testHandler{
		name: "checker", priority: 100, direction: models.DirectionBoth,
		decision: &Decision{Action: DecisionAllow},
	}

	// Send an outgoing request.
	reqMsg := newTestMsg()
	outCtx := &MessageContext{
		ServerName: "test", Direction: models.DirectionOutgoing, Timestamp: time.Now(),
	}

	pipeline.RegisterHandler(correlationChecker)
	_, _, _ = pipeline.Process(context.Background(), reqMsg, outCtx)

	// Send a matching incoming response.
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

	// No handlers registered — should default to allow.
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

	// Second call should return nil (already consumed).
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
