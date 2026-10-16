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

type Reaction string

const (
	ReactionRetried    Reaction = "retried"
	ReactionAdapted    Reaction = "adapted"
	ReactionContinued  Reaction = "continued"
	ReactionSilent     Reaction = "silent"
	ReactionUnobserved Reaction = "unobserved"
)

type EventSink interface {
	RecordEvent(ctx context.Context, event models.ArenaEvent) error
}

type Coverage struct {
	Injected   int `json:"injected"`
	Observed   int `json:"observed"`
	Unresolved int `json:"unresolved"`
}

func (c Coverage) Complete() bool {
	return c.Injected > 0 && c.Observed == c.Injected && c.Unresolved == 0
}

type watch struct {
	inj         Injection
	tool        string
	deadline    time.Time
	sawActivity bool
}

type Observer struct {
	window    time.Duration
	events    EventSink
	logger    zerolog.Logger
	now       func() time.Time
	mu        sync.Mutex
	pending   map[string]*watch
	order     []string
	injected  int
	observed  int
	overrides models.ToolCatalog
	listed    models.ToolCatalog
}

type ObserverOption func(*Observer)

func WithWindow(d time.Duration) ObserverOption {
	return func(o *Observer) { o.window = d }
}

func WithObserverClock(now func() time.Time) ObserverOption {
	return func(o *Observer) { o.now = now }
}

func WithAnnotationOverrides(tools map[string]models.ToolAnnotations) ObserverOption {
	return func(o *Observer) {
		if len(tools) == 0 {
			return
		}
		o.overrides = make(models.ToolCatalog, len(tools))
		for name, ann := range tools {
			o.overrides[name] = models.ToolRecord{Annotations: ann, Source: models.AnnotationSourceConfig}
		}
	}
}

const DefaultWindow = 30 * time.Second

func NewObserver(events EventSink, logger zerolog.Logger, opts ...ObserverOption) *Observer {
	o := &Observer{
		window:  DefaultWindow,
		events:  events,
		logger:  logger.With().Str("component", "chaos-observer").Logger(),
		now:     time.Now,
		pending: make(map[string]*watch),
		listed:  make(models.ToolCatalog),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *Observer) RecordInjection(inj Injection) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.injected++
	o.pending[inj.ID] = &watch{
		inj: inj,

		tool:     inj.Tool,
		deadline: o.now().Add(o.window),
	}
	o.order = append(o.order, inj.ID)
}
func (o *Observer) Name() string                { return "arena-observer" }
func (o *Observer) Priority() int               { return 50 }
func (o *Observer) Direction() models.Direction { return models.DirectionBoth }
func (o *Observer) HandleMessage(ctx context.Context, msg *protocol.Message, mctx *proxy.MessageContext) (*proxy.Decision, error) {
	o.observe(ctx, msg, mctx)
	return &proxy.Decision{Action: proxy.DecisionSkip}, nil
}

func (o *Observer) observe(ctx context.Context, msg *protocol.Message, mctx *proxy.MessageContext) {
	o.recordToolList(msg, mctx)

	isAgentAction := mctx != nil &&
		mctx.Direction == models.DirectionOutgoing &&
		msg != nil &&
		msg.Type == protocol.MessageTypeRequest

	var resolved []resolution

	o.mu.Lock()
	now := o.now()

	live := o.order[:0]

	for _, id := range o.order {
		w, ok := o.pending[id]
		if !ok {
			continue
		}
		live = append(live, id)

		if now.After(w.deadline) {
			r := ReactionSilent
			if w.sawActivity {
				r = ReactionContinued
			}
			resolved = append(resolved, resolution{inj: w.inj, reaction: r})
			delete(o.pending, id)
			continue
		}

		if !isAgentAction {
			continue
		}

		if reqID := messageID(msg); reqID != "" && reqID == w.inj.RequestID {
			continue
		}

		method := msg.GetMethod()
		tool := toolName(msg)

		switch {
		case method == w.inj.Method && tool != "" && tool == w.tool:
			resolved = append(resolved, resolution{inj: w.inj, reaction: ReactionRetried, next: tool})
			delete(o.pending, id)
		case method == string(protocol.MCPMethodToolsCall):
			resolved = append(resolved, resolution{inj: w.inj, reaction: ReactionAdapted, next: tool})
			delete(o.pending, id)
		default:
			w.sawActivity = true
		}
	}

	kept := live[:0]
	for _, id := range live {
		if _, stillPending := o.pending[id]; stillPending {
			kept = append(kept, id)
		}
	}
	o.order = kept

	o.observed += len(resolved)
	var tools models.ToolCatalog
	if len(resolved) > 0 {
		tools = o.toolsLocked()
	}
	o.mu.Unlock()

	for _, r := range resolved {
		o.emit(ctx, r, tools)
	}
}

type resolution struct {
	inj      Injection
	reaction Reaction
	next     string
}

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
		resolved = append(resolved, resolution{inj: w.inj, reaction: r})
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
	tools := o.toolsLocked()
	o.mu.Unlock()

	for _, r := range resolved {
		if r.reaction == ReactionUnobserved {
			o.logger.Debug().
				Str("injection", r.inj.ID).
				Str("gremlin", r.inj.GremlinName).
				Msg("injection left unobserved, excluded from scoring")
			continue
		}
		o.emit(ctx, r, tools)
	}

	return cov
}

func (o *Observer) Coverage() Coverage {
	o.mu.Lock()
	defer o.mu.Unlock()
	return Coverage{
		Injected:   o.injected,
		Observed:   o.observed,
		Unresolved: len(o.pending),
	}
}

func (o *Observer) Tools() models.ToolCatalog {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.toolsLocked()
}

func (o *Observer) toolsLocked() models.ToolCatalog {
	if len(o.listed)+len(o.overrides) == 0 {
		return nil
	}
	out := make(models.ToolCatalog, len(o.listed)+len(o.overrides))
	for name, rec := range o.listed {
		out[name] = rec
	}
	for name, rec := range o.overrides {
		out[name] = rec
	}
	return out
}

func (o *Observer) recordToolList(msg *protocol.Message, mctx *proxy.MessageContext) {
	if msg == nil || mctx == nil || mctx.Direction != models.DirectionIncoming ||
		msg.Type != protocol.MessageTypeResponse || msg.Response == nil || len(msg.Response.Result) == 0 ||
		mctx.CorrelatedRequest == nil || mctx.CorrelatedRequest.GetMethod() != string(protocol.MCPMethodToolsList) {
		return
	}
	listed, err := parseToolList(msg.Response.Result)
	if err != nil {
		o.logger.Debug().Err(err).Msg("tools/list result not readable, annotations not recorded")
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	for _, t := range listed {
		if !t.annotations.Declared() {
			delete(o.listed, t.name)
			continue
		}
		o.listed[t.name] = models.ToolRecord{Annotations: t.annotations, Source: models.AnnotationSourceServer}
	}
}

type listedTool struct {
	name        string
	annotations models.ToolAnnotations
}

func parseToolList(result json.RawMessage) ([]listedTool, error) {
	var page struct {
		Tools []struct {
			Name        string          `json:"name"`
			Annotations json.RawMessage `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &page); err != nil {
		return nil, fmt.Errorf("decoding tools/list result: %w", err)
	}
	out := make([]listedTool, 0, len(page.Tools))
	for _, t := range page.Tools {
		if t.Name == "" {
			continue
		}
		var ann models.ToolAnnotations
		if len(t.Annotations) > 0 && json.Unmarshal(t.Annotations, &ann) != nil {
			ann = models.ToolAnnotations{}
		}
		out = append(out, listedTool{name: t.Name, annotations: ann})
	}
	return out, nil
}

func (o *Observer) emit(ctx context.Context, r resolution, tools models.ToolCatalog) {
	event, err := resolvedEvent(r.inj, r.reaction, r.next, tools)
	if err != nil {
		o.logger.Warn().Err(err).Str("injection", r.inj.ID).Msg("marshalling event details")
	}

	o.logger.Debug().
		Str("gremlin", r.inj.GremlinName).
		Str("reaction", string(r.reaction)).
		Str("outcome", string(event.Outcome)).
		Int("score", event.Score).
		Msg("injection resolved")

	if o.events == nil {
		return
	}
	if err := o.events.RecordEvent(ctx, event); err != nil {
		o.logger.Warn().Err(err).Str("injection", r.inj.ID).Msg("recording arena event")
	}
}

func resolvedEvent(inj Injection, r Reaction, next string, tools models.ToolCatalog) (models.ArenaEvent, error) {
	details := models.EventDetails{
		Gremlin:       inj.GremlinName,
		Reaction:      string(r),
		Tool:          inj.Tool,
		Method:        inj.Method,
		NextTool:      next,
		ToolSucceeded: inj.Succeeded,
		Placement:     inj.Placement,
	}
	outcome, score := classify(inj.GremlinName, &details, tools)
	event := models.ArenaEvent{
		ID:          inj.ID,
		GremlinType: inj.GremlinName,
		InjectedAt:  inj.InjectedAt,
		Outcome:     outcome,
		Score:       score,
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return event, fmt.Errorf("encoding details of %s: %w", inj.ID, err)
	}
	event.Details = encoded
	return event, nil
}

func classify(gremlin string, d *models.EventDetails, tools models.ToolCatalog) (outcome models.ArenaOutcome, score int) {
	d.ToolEffect = tools.Effect(d.Tool)
	next := models.EffectUnknown
	d.NextEffect = ""
	if d.NextTool != "" {
		next = tools.Effect(d.NextTool)
		d.NextEffect = next
	}
	return outcomeFor(gremlin, Reaction(d.Reaction), d.ToolEffect, next, d.ToolSucceeded)
}

func Rescore(events []models.ArenaEvent, tools models.ToolCatalog) []models.ArenaEvent {
	out := make([]models.ArenaEvent, len(events))
	for i := range events {
		e := events[i]
		out[i] = e
		if e.Outcome == models.OutcomeUnmeasured || len(e.Details) == 0 {
			continue
		}
		var d models.EventDetails
		if json.Unmarshal(e.Details, &d) != nil || d.Reaction == "" {
			continue
		}
		outcome, score := classify(e.GremlinType, &d, tools)
		encoded, err := json.Marshal(d)
		if err != nil {
			continue
		}
		out[i].Outcome = outcome
		out[i].Score = score
		out[i].Details = encoded
	}
	return out
}

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

func (c Coverage) String() string {
	return fmt.Sprintf("injected=%d observed=%d unresolved=%d complete=%t",
		c.Injected, c.Observed, c.Unresolved, c.Complete())
}
