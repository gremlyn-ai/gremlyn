package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
)

type GremlinHandler struct {
	gremlins []gremlins.Gremlin
	targets  []string
	sink     InjectionSink
	logger   zerolog.Logger
	now      func() time.Time
	newID    func() string
}

const toolCallMethod = "tools/call"

type HandlerOption func(*GremlinHandler)

func WithClock(now func() time.Time) HandlerOption {
	return func(h *GremlinHandler) { h.now = now }
}

func WithIDGenerator(newID func() string) HandlerOption {
	return func(h *GremlinHandler) { h.newID = newID }
}

func WithTargets(names []string) HandlerOption {
	return func(h *GremlinHandler) { h.targets = slices.Clone(names) }
}

func NewGremlinHandler(ordered []gremlins.Gremlin, sink InjectionSink, logger zerolog.Logger, opts ...HandlerOption) *GremlinHandler {
	h := &GremlinHandler{
		gremlins: ordered,
		sink:     sink,
		logger:   logger.With().Str("component", "gremlin-handler").Logger(),
		now:      time.Now,
		newID:    func() string { return uuid.New().String() },
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}
func (h *GremlinHandler) Name() string                { return "arena-gremlins" }
func (h *GremlinHandler) Priority() int               { return 100 }
func (h *GremlinHandler) Direction() models.Direction { return models.DirectionBoth }
func (h *GremlinHandler) HandleMessage(ctx context.Context, msg *protocol.Message, mctx *proxy.MessageContext) (*proxy.Decision, error) {
	if err := ctx.Err(); err != nil {
		return &proxy.Decision{Action: proxy.DecisionSkip}, nil
	}

	if !isToolCall(msg, mctx) {
		return &proxy.Decision{Action: proxy.DecisionSkip}, nil
	}

	if h.targets != nil && !h.aimed(msg, mctx) {
		return &proxy.Decision{Action: proxy.DecisionSkip}, nil
	}

	for _, g := range h.gremlins {
		modified, injected, err := g.Inject(ctx, msg)
		if err != nil {
			h.logger.Warn().Err(err).
				Str("gremlin", g.Name()).
				Msg("gremlin injection failed, passing message through untouched")
			continue
		}
		if !injected {
			continue
		}
		if modified == nil {
			h.logger.Error().
				Str("gremlin", g.Name()).
				Msg("gremlin reported an injection but returned no message, ignoring")
			continue
		}

		h.record(g.Name(), msg, modified, mctx)

		return &proxy.Decision{
			Action:          proxy.DecisionModify,
			ModifiedMessage: modified,
			Reason:          fmt.Sprintf("gremlin:%s", g.Name()),
			Metadata:        map[string]string{"gremlin": g.Name()},
		}, nil
	}
	return &proxy.Decision{Action: proxy.DecisionSkip}, nil
}

func (h *GremlinHandler) record(name string, original, modified *protocol.Message, mctx *proxy.MessageContext) {
	if h.sink == nil {
		return
	}

	inj := Injection{
		ID:          h.newID(),
		GremlinName: name,
		Direction:   mctx.Direction,
		RequestID:   messageID(original),
		Method:      correlatedMethod(original, mctx),
		Tool:        correlatedTool(original, mctx),
		Succeeded:   succeeded(original),
		InjectedAt:  h.now(),
		Original:    original,
		Modified:    modified,
	}
	h.sink.RecordInjection(inj)
}

func (h *GremlinHandler) aimed(msg *protocol.Message, mctx *proxy.MessageContext) bool {
	return slices.Contains(h.targets, correlatedTool(msg, mctx))
}

func isToolCall(msg *protocol.Message, mctx *proxy.MessageContext) bool {
	if msg != nil && msg.GetMethod() == toolCallMethod {
		return true
	}
	return mctx != nil && mctx.CorrelatedRequest != nil &&
		mctx.CorrelatedRequest.GetMethod() == toolCallMethod
}

func succeeded(msg *protocol.Message) bool {
	if msg == nil || msg.Type != protocol.MessageTypeResponse || msg.Response == nil ||
		msg.Response.Error != nil || len(msg.Response.Result) == 0 {
		return false
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	if json.Unmarshal(msg.Response.Result, &result) != nil {
		return true
	}
	return !result.IsError
}

func messageID(msg *protocol.Message) string {
	if msg == nil {
		return ""
	}
	id, ok := msg.GetID()
	if !ok {
		return ""
	}
	return id.String()
}

func correlatedTool(msg *protocol.Message, mctx *proxy.MessageContext) string {
	if t := toolName(msg); t != "" {
		return t
	}
	if mctx != nil && mctx.CorrelatedRequest != nil {
		return toolName(mctx.CorrelatedRequest)
	}
	return ""
}

func correlatedMethod(msg *protocol.Message, mctx *proxy.MessageContext) string {
	if msg != nil {
		if m := msg.GetMethod(); m != "" {
			return m
		}
	}
	if mctx != nil && mctx.CorrelatedRequest != nil {
		return mctx.CorrelatedRequest.GetMethod()
	}
	return ""
}
