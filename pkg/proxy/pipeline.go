package proxy

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

type DecisionAction string

const (
	DecisionAllow  DecisionAction = "allow"
	DecisionBlock  DecisionAction = "block"
	DecisionRedact DecisionAction = "redact"
	DecisionModify DecisionAction = "modify"
	DecisionSkip   DecisionAction = "skip"
)

type Decision struct {
	Action          DecisionAction    `json:"action"`
	ModifiedMessage *protocol.Message `json:"modified_message,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	RuleID          string            `json:"rule_id,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type MessageContext struct {
	ServerName        string              `json:"server_name"`
	Direction         models.Direction    `json:"direction"`
	Timestamp         time.Time           `json:"timestamp"`
	RequestID         *protocol.JSONRPCID `json:"request_id,omitempty"`
	CorrelatedRequest *protocol.Message   `json:"correlated_request,omitempty"`
}

type Handler interface {
	Name() string
	Priority() int
	Direction() models.Direction
	HandleMessage(ctx context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error)
}

type Pipeline struct {
	handlers   []Handler
	mu         sync.RWMutex
	correlator *RequestCorrelator
	logger     zerolog.Logger
}

func NewPipeline(logger zerolog.Logger) *Pipeline {
	return &Pipeline{
		correlator: NewRequestCorrelator(5 * time.Minute),
		logger:     logger,
	}
}

func (p *Pipeline) callHandler(
	ctx context.Context,
	h Handler,
	msg *protocol.Message,
	mctx *MessageContext,
) (decision *Decision, err error) {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error().
				Str("handler", h.Name()).
				Interface("panic", r).
				Bytes("stack", debug.Stack()).
				Msg("handler panicked, containing it")
			decision = nil
			err = fmt.Errorf("handler %q panicked: %v", h.Name(), r)
		}
	}()

	return h.HandleMessage(ctx, msg, mctx)
}

func (p *Pipeline) Close() {
	if p.correlator != nil {
		p.correlator.Close()
	}
}

func (p *Pipeline) RegisterHandler(h Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.handlers = append(p.handlers, h)
	sort.Slice(p.handlers, func(i, j int) bool {
		return p.handlers[i].Priority() < p.handlers[j].Priority()
	})

	p.logger.Info().Str("handler", h.Name()).Int("priority", h.Priority()).Msg("handler registered")
}

func (p *Pipeline) Process(ctx context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, *protocol.Message, error) {
	if mctx.Direction == models.DirectionOutgoing && msg.Type == protocol.MessageTypeRequest {
		p.correlator.TrackRequest(msg)
	}

	if mctx.Direction == models.DirectionIncoming && msg.Type == protocol.MessageTypeResponse {
		correlated := p.correlator.CorrelateResponse(msg)
		if correlated != nil {
			mctx.CorrelatedRequest = correlated
		}
	}

	p.mu.RLock()
	handlers := make([]Handler, len(p.handlers))
	copy(handlers, p.handlers)
	p.mu.RUnlock()

	currentMsg := msg
	var finalDecision *Decision

	for _, h := range handlers {
		if !h.Direction().Matches(mctx.Direction) {
			continue
		}

		decision, err := p.callHandler(ctx, h, currentMsg, mctx)
		if err != nil {
			p.logger.Warn().Err(err).Str("handler", h.Name()).Msg("handler error, continuing")
			continue
		}
		if decision == nil {
			continue
		}

		switch decision.Action {
		case DecisionBlock:
			p.logger.Info().
				Str("handler", h.Name()).
				Str("reason", decision.Reason).
				Msg("message blocked")

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
		}
	}

	if finalDecision == nil {
		finalDecision = &Decision{Action: DecisionAllow}
	}

	return finalDecision, currentMsg, nil
}

type RequestCorrelator struct {
	pending  sync.Map
	ttl      time.Duration
	stop     chan struct{}
	stopOnce sync.Once
}

type correlatedEntry struct {
	msg       *protocol.Message
	timestamp time.Time
}

func NewRequestCorrelator(ttl time.Duration) *RequestCorrelator {
	rc := &RequestCorrelator{ttl: ttl, stop: make(chan struct{})}
	go rc.cleanupLoop()
	return rc
}

func (rc *RequestCorrelator) Close() {
	rc.stopOnce.Do(func() { close(rc.stop) })
}

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
	for {
		select {
		case <-rc.stop:
			return
		case <-ticker.C:
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
}

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
