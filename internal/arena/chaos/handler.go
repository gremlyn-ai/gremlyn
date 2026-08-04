package chaos

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
)

// GremlinHandler adapts Arena's gremlins to proxy.Handler, so chaos is injected
// into real MCP traffic flowing through the shared proxy pipeline.
//
// It runs late (see Priority) so that Shield, if also registered, has already
// decided on the message: a gremlin must not be able to smuggle a payload past a
// policy that would have blocked it.
type GremlinHandler struct {
	// gremlins is an ORDERED slice, never a map or Registry.List(): map
	// iteration order is random, and a session whose injection order varies
	// between runs cannot be replayed, which makes its score meaningless.
	gremlins []gremlins.Gremlin
	sink     InjectionSink
	logger   zerolog.Logger
	now      func() time.Time
	newID    func() string
}

// HandlerOption configures a GremlinHandler.
type HandlerOption func(*GremlinHandler)

// WithClock overrides the time source. Tests use this to keep injection records
// deterministic.
func WithClock(now func() time.Time) HandlerOption {
	return func(h *GremlinHandler) { h.now = now }
}

// WithIDGenerator overrides injection ID generation. Tests use this to keep
// injection records deterministic.
func WithIDGenerator(newID func() string) HandlerOption {
	return func(h *GremlinHandler) { h.newID = newID }
}

// NewGremlinHandler builds a handler for an ordered set of gremlins.
//
// The order is part of the session's identity: given the same seed and the same
// message sequence, the same gremlin must fire on the same message. Callers must
// pass gremlins in a stable order (the order the session config lists them),
// not whatever Registry.List returns.
//
// sink may be nil, in which case injections are logged and discarded.
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

// Name returns the handler's pipeline identifier.
func (h *GremlinHandler) Name() string { return "arena-gremlins" }

// Priority returns 100 — chaos runs after Shield's policy (priority 10), so a
// gremlin can never bypass a decision that would have blocked the message.
func (h *GremlinHandler) Priority() int { return 100 }

// Direction returns both: some gremlins alter outgoing tool calls, others alter
// incoming results. Each gremlin decides for itself whether a given message is
// one it acts on.
func (h *GremlinHandler) Direction() models.Direction { return models.DirectionBoth }

// HandleMessage offers the message to each gremlin in order and returns the first
// mutation produced.
//
// At most one gremlin injects per message. Letting several compose would make the
// agent's reaction impossible to attribute to a cause, and attribution is the
// whole point of the resilience score.
func (h *GremlinHandler) HandleMessage(ctx context.Context, msg *protocol.Message, mctx *proxy.MessageContext) (*proxy.Decision, error) {
	if err := ctx.Err(); err != nil {
		return &proxy.Decision{Action: proxy.DecisionSkip}, nil
	}

	for _, g := range h.gremlins {
		modified, injected, err := g.Inject(ctx, msg)
		if err != nil {
			// A gremlin's own failure must never break the user's traffic. Arena
			// is a testing tool; if it breaks the agent by accident, every result
			// it produces afterwards is noise. Log and move on.
			h.logger.Warn().Err(err).
				Str("gremlin", g.Name()).
				Msg("gremlin injection failed, passing message through untouched")
			continue
		}
		if !injected {
			continue
		}
		if modified == nil {
			// Contract violation: a gremlin claiming an injection must return the
			// altered message. Forwarding nil here would drop the message and
			// hang the agent.
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

// record hands the injection to the sink.
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
		InjectedAt:  h.now(),
		Original:    original,
		Modified:    modified,
	}
	h.sink.RecordInjection(inj)
}

// messageID returns the JSON-RPC id as a string, or "" for a notification.
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

// correlatedMethod resolves the method a message belongs to.
//
// A response carries no method of its own, so it is taken from the request the
// pipeline's correlator matched it to — without that, an injection on a tool
// result could not be attributed to the tool that was called.
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
