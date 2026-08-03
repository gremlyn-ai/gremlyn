package proxy

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

// DecisionAction represents what the pipeline decided to do with a message.
type DecisionAction string

const (
	// DecisionAllow lets the message through unchanged.
	DecisionAllow DecisionAction = "allow"
	// DecisionBlock rejects the message.
	DecisionBlock DecisionAction = "block"
	// DecisionRedact sanitizes the message before forwarding.
	DecisionRedact DecisionAction = "redact"
	// DecisionModify alters the message before forwarding.
	DecisionModify DecisionAction = "modify"
	// DecisionSkip means the handler has no opinion on this message.
	DecisionSkip DecisionAction = "skip"
)

// Decision represents the result of a handler processing a message.
type Decision struct {
	Action          DecisionAction    `json:"action"`
	ModifiedMessage *protocol.Message `json:"modified_message,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	RuleID          string            `json:"rule_id,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// MessageContext provides context about a message being processed by the pipeline.
type MessageContext struct {
	ServerName        string              `json:"server_name"`
	Direction         models.Direction    `json:"direction"`
	Timestamp         time.Time           `json:"timestamp"`
	RequestID         *protocol.JSONRPCID `json:"request_id,omitempty"`
	CorrelatedRequest *protocol.Message   `json:"correlated_request,omitempty"`
}

// Handler is the interface for synchronous pipeline handlers.
// Shield registers policy handlers, detection handlers, etc.
// Arena registers gremlin injection handlers.
type Handler interface {
	// Name returns a unique identifier for this handler.
	Name() string
	// Priority returns the execution order (lower number = runs first).
	Priority() int
	// Direction returns which direction(s) this handler processes.
	Direction() models.Direction
	// HandleMessage processes a message and returns a decision.
	HandleMessage(ctx context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error)
}

// AsyncHandler extends Handler with post-forwarding async processing.
// Used for logging, alerting, ML analysis, and other non-blocking work.
type AsyncHandler interface {
	Handler
	// HandleAsync is called after the message has been forwarded or blocked.
	// Runs in a separate goroutine. Receives the final decision.
	HandleAsync(ctx context.Context, msg *protocol.Message, mctx *MessageContext, decision *Decision)
}

// Pipeline processes intercepted MCP messages through registered handlers.
// It is the hook system that enables Shield and Arena to plug into the proxy.
type Pipeline struct {
	handlers      []Handler
	asyncHandlers []AsyncHandler
	mu            sync.RWMutex
	correlator    *RequestCorrelator
	logger        zerolog.Logger
}

// NewPipeline creates a new Pipeline with the given logger.
func NewPipeline(logger zerolog.Logger) *Pipeline {
	return &Pipeline{
		correlator: NewRequestCorrelator(5 * time.Minute),
		logger:     logger,
	}
}

// RegisterHandler adds a synchronous handler to the pipeline, sorted by priority.
func (p *Pipeline) RegisterHandler(h Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.handlers = append(p.handlers, h)
	sort.Slice(p.handlers, func(i, j int) bool {
		return p.handlers[i].Priority() < p.handlers[j].Priority()
	})

	p.logger.Info().Str("handler", h.Name()).Int("priority", h.Priority()).Msg("handler registered")
}

// RegisterAsyncHandler adds an async handler to the pipeline.
func (p *Pipeline) RegisterAsyncHandler(h AsyncHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Also add to sync handlers for the HandleMessage phase.
	p.handlers = append(p.handlers, h)
	sort.Slice(p.handlers, func(i, j int) bool {
		return p.handlers[i].Priority() < p.handlers[j].Priority()
	})

	p.asyncHandlers = append(p.asyncHandlers, h)
	p.logger.Info().Str("handler", h.Name()).Msg("async handler registered")
}

// UnregisterHandler removes a handler by name.
func (p *Pipeline) UnregisterHandler(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from sync handlers.
	filtered := make([]Handler, 0, len(p.handlers))
	for _, h := range p.handlers {
		if h.Name() != name {
			filtered = append(filtered, h)
		}
	}
	p.handlers = filtered

	// Remove from async handlers.
	asyncFiltered := make([]AsyncHandler, 0, len(p.asyncHandlers))
	for _, h := range p.asyncHandlers {
		if h.Name() != name {
			asyncFiltered = append(asyncFiltered, h)
		}
	}
	p.asyncHandlers = asyncFiltered

	p.logger.Info().Str("handler", name).Msg("handler unregistered")
}

// Process runs a message through all registered handlers and returns the final decision
// and the (possibly modified) message.
func (p *Pipeline) Process(ctx context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, *protocol.Message, error) {
	// Step 1: Track outgoing requests for correlation.
	if mctx.Direction == models.DirectionOutgoing && msg.Type == protocol.MessageTypeRequest {
		p.correlator.TrackRequest(msg)
	}

	// Step 2: Correlate incoming responses with their original requests.
	if mctx.Direction == models.DirectionIncoming && msg.Type == protocol.MessageTypeResponse {
		correlated := p.correlator.CorrelateResponse(msg)
		if correlated != nil {
			mctx.CorrelatedRequest = correlated
		}
	}

	p.mu.RLock()
	handlers := make([]Handler, len(p.handlers))
	copy(handlers, p.handlers)
	asyncHandlers := make([]AsyncHandler, len(p.asyncHandlers))
	copy(asyncHandlers, p.asyncHandlers)
	p.mu.RUnlock()

	// Step 3: Run sync handlers in priority order.
	currentMsg := msg
	var finalDecision *Decision

	for _, h := range handlers {
		// Direction filtering.
		if !h.Direction().Matches(mctx.Direction) {
			continue
		}

		decision, err := h.HandleMessage(ctx, currentMsg, mctx)
		if err != nil {
			p.logger.Warn().Err(err).Str("handler", h.Name()).Msg("handler error, continuing")
			continue
		}
		if decision == nil {
			continue
		}

		switch decision.Action {
		case DecisionBlock:
			// Block immediately — short circuit.
			p.logger.Info().
				Str("handler", h.Name()).
				Str("reason", decision.Reason).
				Msg("message blocked")

			// Fan out to async handlers with the block decision.
			for _, ah := range asyncHandlers {
				go ah.HandleAsync(ctx, currentMsg, mctx, decision)
			}
			return decision, nil, nil

		case DecisionRedact, DecisionModify:
			if decision.ModifiedMessage != nil {
				currentMsg = decision.ModifiedMessage
			}
			finalDecision = decision

		case DecisionAllow:
			if finalDecision == nil {
				finalDecision = decision
			}

		case DecisionSkip:
			// No opinion, continue.
		}
	}

	if finalDecision == nil {
		finalDecision = &Decision{Action: DecisionAllow}
	}

	// Step 4: Fan out to async handlers.
	for _, ah := range asyncHandlers {
		go ah.HandleAsync(ctx, currentMsg, mctx, finalDecision)
	}

	return finalDecision, currentMsg, nil
}

// HandlerCount returns the number of registered handlers (for testing/status).
func (p *Pipeline) HandlerCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.handlers)
}

// --- Request Correlator ---

// RequestCorrelator tracks in-flight JSON-RPC requests to correlate them with responses.
type RequestCorrelator struct {
	pending sync.Map
	ttl     time.Duration
}

type correlatedEntry struct {
	msg       *protocol.Message
	timestamp time.Time
}

// NewRequestCorrelator creates a correlator with the given TTL for pending requests.
func NewRequestCorrelator(ttl time.Duration) *RequestCorrelator {
	rc := &RequestCorrelator{ttl: ttl}
	go rc.cleanupLoop()
	return rc
}

// TrackRequest records an outgoing request for later correlation.
func (rc *RequestCorrelator) TrackRequest(msg *protocol.Message) {
	if msg.Type != protocol.MessageTypeRequest || msg.Request == nil {
		return
	}
	key := msg.Request.ID.String()
	rc.pending.Store(key, &correlatedEntry{
		msg:       msg,
		timestamp: time.Now(),
	})
}

// CorrelateResponse finds and removes the original request for a response.
func (rc *RequestCorrelator) CorrelateResponse(msg *protocol.Message) *protocol.Message {
	if msg.Type != protocol.MessageTypeResponse || msg.Response == nil {
		return nil
	}
	key := msg.Response.ID.String()
	val, ok := rc.pending.LoadAndDelete(key)
	if !ok {
		return nil
	}
	entry := val.(*correlatedEntry)
	return entry.msg
}

func (rc *RequestCorrelator) cleanupLoop() {
	ticker := time.NewTicker(rc.ttl)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		rc.pending.Range(func(key, value any) bool {
			entry := value.(*correlatedEntry)
			if now.Sub(entry.timestamp) > rc.ttl {
				rc.pending.Delete(key)
			}
			return true
		})
	}
}

// --- Helper for creating error responses ---

// NewBlockErrorResponse creates a JSON-RPC error response for a blocked request.
func NewBlockErrorResponse(requestID protocol.JSONRPCID, reason string) *protocol.Message {
	return &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      requestID,
			Error: &protocol.JSONRPCError{
				Code:    -32600,
				Message: fmt.Sprintf("Blocked by Gremlyn: %s", reason),
			},
		},
	}
}
