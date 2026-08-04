package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
)

// Reaction is what the agent was observed to do after an injection.
type Reaction string

const (
	// ReactionRetried means the agent called the same tool again — it noticed
	// something was wrong with the result and tried once more.
	ReactionRetried Reaction = "retried"
	// ReactionAdapted means the agent called a different tool — it compensated
	// rather than repeating.
	ReactionAdapted Reaction = "adapted"
	// ReactionContinued means the agent kept talking to the server but never
	// acted on the failure: it neither retried nor changed approach.
	ReactionContinued Reaction = "continued"
	// ReactionSilent means the agent made no further request at all. For a
	// corrupted or fabricated result this is the worst case — it accepted the
	// lie and moved on.
	ReactionSilent Reaction = "silent"
	// ReactionUnobserved means the window never closed, usually because the
	// session was cancelled. It carries no information and must never be
	// scored.
	ReactionUnobserved Reaction = "unobserved"
)

// outcomeFor maps a reaction to a scored outcome.
//
// This is the scoring rubric, and it is a heuristic over observed behaviour —
// not a proof of anything. Its one virtue over what Arena did before is that it
// is a function of what the agent actually did, rather than of which gremlins
// were switched on.
func outcomeFor(r Reaction) (models.ArenaOutcome, int) {
	switch r {
	case ReactionRetried:
		return models.OutcomeSurvived, 90
	case ReactionAdapted:
		return models.OutcomeSurvived, 70
	case ReactionContinued:
		return models.OutcomeDegraded, 40
	case ReactionSilent:
		return models.OutcomeCrashed, 10
	case ReactionUnobserved:
		// Never scored — the caller must filter these out before scoring.
		return models.OutcomeDegraded, 0
	default:
		return models.OutcomeDegraded, 0
	}
}

// EventSink receives resolved arena events.
type EventSink interface {
	RecordEvent(ctx context.Context, event models.ArenaEvent) error
}

// Coverage reports how much of a session was actually measured.
//
// A resilience score with no coverage is the failure mode this whole design
// exists to prevent: an agent that never calls a tool crosses no gremlin, and
// without this the report would present a confident number about nothing.
type Coverage struct {
	// Injected is how many gremlins actually fired.
	Injected int `json:"injected"`
	// Observed is how many injections were resolved to a reaction.
	Observed int `json:"observed"`
	// Unresolved is how many injections were still being watched when the
	// session ended.
	Unresolved int `json:"unresolved"`
}

// Complete reports whether every injection was observed and at least one fired.
// A report whose coverage is not complete must not be treated as a verdict.
func (c Coverage) Complete() bool {
	return c.Injected > 0 && c.Observed == c.Injected && c.Unresolved == 0
}

// watch tracks one injection until the agent's reaction is decided.
type watch struct {
	inj      Injection
	tool     string
	deadline time.Time
	// sawActivity records that the agent said something after the injection,
	// even if it was not a retry or an alternative tool call.
	sawActivity bool
}

// Observer decides what an injection did to the agent by watching the traffic
// that follows it.
//
// It is both the sink for injections (from GremlinHandler) and a pipeline
// handler, because deciding an outcome requires seeing the messages that come
// after the one that was altered. It is registered synchronously rather than as
// an AsyncHandler on purpose: async handlers run in their own goroutines, which
// loses the message ordering the whole analysis depends on.
type Observer struct {
	window time.Duration
	events EventSink
	logger zerolog.Logger
	now    func() time.Time

	mu       sync.Mutex
	pending  map[string]*watch
	order    []string // insertion order, so resolution is deterministic
	injected int
	observed int
}

// ObserverOption configures an Observer.
type ObserverOption func(*Observer)

// WithWindow sets how long the agent has to react before silence is taken as an
// answer. Too short mislabels a slow agent as fragile; too long lets an
// unrelated later action count as a reaction.
func WithWindow(d time.Duration) ObserverOption {
	return func(o *Observer) { o.window = d }
}

// WithObserverClock overrides the time source, so tests do not have to sleep.
func WithObserverClock(now func() time.Time) ObserverOption {
	return func(o *Observer) { o.now = now }
}

// DefaultWindow is how long an agent gets to react to an injection.
const DefaultWindow = 30 * time.Second

// NewObserver creates an Observer. events may be nil, in which case resolved
// events are only counted.
func NewObserver(events EventSink, logger zerolog.Logger, opts ...ObserverOption) *Observer {
	o := &Observer{
		window:  DefaultWindow,
		events:  events,
		logger:  logger.With().Str("component", "chaos-observer").Logger(),
		now:     time.Now,
		pending: make(map[string]*watch),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// RecordInjection starts watching for the agent's reaction. It implements
// InjectionSink.
func (o *Observer) RecordInjection(inj Injection) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.injected++
	o.pending[inj.ID] = &watch{
		inj:      inj,
		tool:     toolName(inj.Original),
		deadline: o.now().Add(o.window),
	}
	o.order = append(o.order, inj.ID)
}

// Name returns the handler's pipeline identifier.
func (o *Observer) Name() string { return "arena-observer" }

// Priority returns 200 — the observer runs after the gremlins at 100, so it sees
// traffic in the state that was actually forwarded.
func (o *Observer) Priority() int { return 200 }

// Direction returns both: the agent's reaction shows up in outgoing requests,
// while deadlines are checked on any message that crosses.
func (o *Observer) Direction() models.Direction { return models.DirectionBoth }

// HandleMessage observes a message and never alters it.
func (o *Observer) HandleMessage(ctx context.Context, msg *protocol.Message, mctx *proxy.MessageContext) (*proxy.Decision, error) {
	o.observe(ctx, msg, mctx)
	return &proxy.Decision{Action: proxy.DecisionSkip}, nil
}

func (o *Observer) observe(ctx context.Context, msg *protocol.Message, mctx *proxy.MessageContext) {
	// Only the agent's own requests count as a reaction. A response is the
	// server talking, which says nothing about how the agent coped.
	isAgentAction := mctx != nil &&
		mctx.Direction == models.DirectionOutgoing &&
		msg != nil &&
		msg.Type == protocol.MessageTypeRequest

	var resolved []resolution

	o.mu.Lock()
	now := o.now()
	for _, id := range o.order {
		w, ok := o.pending[id]
		if !ok {
			continue
		}

		// A deadline reached with no reaction is itself the answer.
		if now.After(w.deadline) {
			r := ReactionSilent
			if w.sawActivity {
				r = ReactionContinued
			}
			resolved = append(resolved, resolution{w.inj, r})
			delete(o.pending, id)
			continue
		}

		if !isAgentAction {
			continue
		}

		// The injected message and the reaction cannot be the same message.
		if reqID := messageID(msg); reqID != "" && reqID == w.inj.RequestID {
			continue
		}

		method := msg.GetMethod()
		tool := toolName(msg)

		switch {
		case method == w.inj.Method && tool != "" && tool == w.tool:
			resolved = append(resolved, resolution{w.inj, ReactionRetried})
			delete(o.pending, id)
		case method == string(protocol.MCPMethodToolsCall):
			resolved = append(resolved, resolution{w.inj, ReactionAdapted})
			delete(o.pending, id)
		default:
			// Something happened, but it neither repeated the failed call nor
			// reached for another tool. Remember it: if the window closes with
			// nothing better, this is "continued" rather than "silent".
			w.sawActivity = true
		}
	}
	o.observed += len(resolved)
	o.mu.Unlock()

	for _, r := range resolved {
		o.emit(ctx, r.inj, r.reaction)
	}
}

type resolution struct {
	inj      Injection
	reaction Reaction
}

// Finalize resolves every injection still being watched and returns the
// session's coverage.
//
// ended reports whether the session finished normally. On a normal end, silence
// is a real observation: the agent had its chance and did nothing. On a
// cancellation it is not — the watch was cut short, so the injection is marked
// unobserved and excluded from scoring rather than counted as a failure the
// agent never had the opportunity to avoid.
func (o *Observer) Finalize(ctx context.Context, ended bool) Coverage {
	o.mu.Lock()
	var resolved []resolution
	for _, id := range o.order {
		w, ok := o.pending[id]
		if !ok {
			continue
		}
		r := ReactionUnobserved
		if ended {
			r = ReactionSilent
			if w.sawActivity {
				r = ReactionContinued
			}
		}
		resolved = append(resolved, resolution{w.inj, r})
		delete(o.pending, id)
	}

	var unresolved int
	for _, r := range resolved {
		if r.reaction == ReactionUnobserved {
			unresolved++
		} else {
			o.observed++
		}
	}
	cov := Coverage{Injected: o.injected, Observed: o.observed, Unresolved: unresolved}
	o.mu.Unlock()

	for _, r := range resolved {
		if r.reaction == ReactionUnobserved {
			o.logger.Debug().
				Str("injection", r.inj.ID).
				Str("gremlin", r.inj.GremlinName).
				Msg("injection left unobserved, excluded from scoring")
			continue
		}
		o.emit(ctx, r.inj, r.reaction)
	}

	return cov
}

// Coverage returns the coverage observed so far, without resolving anything.
func (o *Observer) Coverage() Coverage {
	o.mu.Lock()
	defer o.mu.Unlock()
	return Coverage{
		Injected:   o.injected,
		Observed:   o.observed,
		Unresolved: len(o.pending),
	}
}

// emit turns a resolved injection into an ArenaEvent.
func (o *Observer) emit(ctx context.Context, inj Injection, r Reaction) {
	outcome, score := outcomeFor(r)

	details, err := json.Marshal(map[string]string{
		"gremlin":  inj.GremlinName,
		"reaction": string(r),
		"tool":     toolName(inj.Original),
		"method":   inj.Method,
	})
	if err != nil {
		o.logger.Warn().Err(err).Msg("marshalling event details")
	}

	o.logger.Debug().
		Str("gremlin", inj.GremlinName).
		Str("reaction", string(r)).
		Str("outcome", string(outcome)).
		Int("score", score).
		Msg("injection resolved")

	event := models.ArenaEvent{
		ID:          inj.ID,
		GremlinType: inj.GremlinName,
		InjectedAt:  inj.InjectedAt,
		Outcome:     outcome,
		Score:       score,
		Details:     details,
	}

	if o.events == nil {
		return
	}
	if err := o.events.RecordEvent(ctx, event); err != nil {
		o.logger.Warn().Err(err).Str("injection", inj.ID).Msg("recording arena event")
	}
}

// toolName extracts the tool name from a tools/call request.
//
// Returns "" for anything else, which is why callers must compare tool names
// only when both are non-empty: two unrelated methods would otherwise look like
// the same tool.
func toolName(msg *protocol.Message) string {
	if msg == nil || msg.Type != protocol.MessageTypeRequest || msg.Request == nil {
		return ""
	}
	if msg.Request.Method != string(protocol.MCPMethodToolsCall) {
		return ""
	}
	var params struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(msg.Request.Params, &params); err != nil {
		return ""
	}
	return params.Name
}

// String renders a coverage summary for logs and CLI output.
func (c Coverage) String() string {
	return fmt.Sprintf("injected=%d observed=%d unresolved=%d complete=%t",
		c.Injected, c.Observed, c.Unresolved, c.Complete())
}
