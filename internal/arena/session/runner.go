package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

// Runner executes chaos sessions by running gremlins against test messages.
type Runner struct {
	registry *gremlins.Registry
	logger   zerolog.Logger
}

// NewRunner creates a Runner with the given gremlin registry.
func NewRunner(registry *gremlins.Registry, logger zerolog.Logger) *Runner {
	return &Runner{
		registry: registry,
		logger:   logger,
	}
}

// Run executes a chaos session: applies configured gremlins to test messages,
// records events, scores results, and completes the session.
func (r *Runner) Run(ctx context.Context, sess *Session, recorder *Recorder) error {
	sess.SetStatus(models.SessionStatusRunning)

	// Resolve gremlins from registry.
	activeGremlins, err := r.resolveGremlins(sess.config)
	if err != nil {
		return err
	}

	r.logger.Info().
		Str("session_id", sess.ID()).
		Int("gremlins", len(activeGremlins)).
		Int("prompts", len(sess.config.Prompts)).
		Msg("starting chaos run")

	prompts := sess.config.Prompts
	if len(prompts) == 0 {
		prompts = defaultPrompts()
	}

	// Run each prompt through each gremlin.
	for _, prompt := range prompts {
		if ctx.Err() != nil {
			break
		}
		for _, g := range activeGremlins {
			if ctx.Err() != nil {
				break
			}
			r.runGremlin(ctx, sess, recorder, g, prompt)
		}
	}

	// Score results.
	events := recorder.Events()
	report := scoring.Score(events, nil)
	resultsJSON, _ := json.Marshal(report)

	sent, survived, crashed := countOutcomes(events)
	sess.Complete(resultsJSON, sent, survived, crashed)

	r.logger.Info().
		Str("session_id", sess.ID()).
		Int("overall_score", report.Overall).
		Str("grade", string(report.Grade)).
		Msg("chaos run completed")

	return nil
}

func (r *Runner) resolveGremlins(cfg Config) ([]gremlins.Gremlin, error) {
	var active []gremlins.Gremlin
	for _, name := range cfg.Gremlins {
		g, ok := r.registry.Get(name)
		if !ok {
			return nil, fmt.Errorf("unknown gremlin: %q", name)
		}
		active = append(active, g)
	}
	return active, nil
}

func (r *Runner) runGremlin(ctx context.Context, sess *Session, recorder *Recorder, g gremlins.Gremlin, prompt string) {
	msg := buildTestMessage(g, prompt)

	modified, injected, err := g.Inject(ctx, msg)
	if err != nil {
		r.logger.Warn().Err(err).
			Str("gremlin", g.Name()).
			Str("session_id", sess.ID()).
			Msg("gremlin injection error")
		return
	}

	if !injected {
		return
	}

	outcome, score := assessOutcome(g.Name(), msg, modified)

	configJSON, _ := json.Marshal(map[string]string{"prompt": prompt})
	responseJSON, _ := json.Marshal(map[string]string{"injected": "true"})
	detailsJSON, _ := json.Marshal(map[string]string{
		"gremlin": g.Name(),
		"outcome": string(outcome),
	})

	event := models.ArenaEvent{
		ID:            uuid.New().String(),
		SessionID:     sess.ID(),
		GremlinType:   g.Name(),
		GremlinConfig: configJSON,
		InjectedAt:    time.Now(),
		AgentResponse: responseJSON,
		Outcome:       outcome,
		Score:         score,
		Details:       detailsJSON,
	}

	if err := recorder.Record(ctx, event); err != nil {
		r.logger.Warn().Err(err).Msg("failed to record event")
	}
}

// buildTestMessage creates an appropriate test message for the gremlin type.
func buildTestMessage(g gremlins.Gremlin, prompt string) *protocol.Message {
	switch g.Name() {
	case "hallucination":
		// Outgoing tool call — hallucination replaces the tool name.
		params, _ := json.Marshal(map[string]any{
			"name":      "search_contacts",
			"arguments": map[string]string{"query": prompt},
		})
		return &protocol.Message{
			Type: protocol.MessageTypeRequest,
			Request: &protocol.JSONRPCRequest{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      protocol.NewIntID(1),
				Method:  string(protocol.MCPMethodToolsCall),
				Params:  params,
			},
		}
	default:
		// Incoming response — latency/corruption/loop modify the response.
		result, _ := json.Marshal(map[string]any{
			"content": prompt,
			"status":  "ok",
			"data":    map[string]string{"field1": "value1", "field2": "value2"},
		})
		return &protocol.Message{
			Type: protocol.MessageTypeResponse,
			Response: &protocol.JSONRPCResponse{
				JSONRPC: protocol.JSONRPCVersion,
				ID:      protocol.NewIntID(1),
				Result:  result,
			},
		}
	}
}

// assessOutcome determines the outcome and score for an injection.
func assessOutcome(gremlinName string, _, modified *protocol.Message) (outcome models.ArenaOutcome, score int) {
	switch gremlinName {
	case "hallucination":
		// Tool name was replaced — agent would face an unknown tool error.
		return models.OutcomeDegraded, 30

	case "latency":
		// Message was delayed but not modified.
		return models.OutcomeSurvived, 70

	case "corruption":
		// Check if modified response is still valid JSON.
		if modified.Response != nil && modified.Response.Result != nil {
			var check any
			if json.Unmarshal(modified.Response.Result, &check) == nil {
				return models.OutcomeDegraded, 50
			}
		}
		return models.OutcomeCrashed, 10

	case "loop":
		// Response replaced with retry message.
		return models.OutcomeDegraded, 40

	default:
		return models.OutcomeDegraded, 50
	}
}

func countOutcomes(events []models.ArenaEvent) (sent, survived, crashed int) {
	sent = len(events)
	for _, e := range events {
		switch e.Outcome {
		case models.OutcomeSurvived:
			survived++
		case models.OutcomeCrashed:
			crashed++
		case models.OutcomeDegraded:
			// Deliberately not counted: a degraded outcome is neither a clean
			// survival nor a crash, and the two returned counters are the only
			// buckets. It stays folded into `sent` only.
		}
	}
	return sent, survived, crashed
}

func defaultPrompts() []string {
	return []string{
		"Search for recent customer orders",
		"Get the user profile for john@example.com",
		"List all active support tickets",
		"Update the billing address for account 12345",
		"Generate a report of monthly revenue",
	}
}
