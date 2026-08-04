package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/shield/detection"
	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
)

// Engine evaluates MCP messages against security rules and detection layers.
// It implements proxy.Handler from pkg/proxy so it can be plugged into the
// analysis pipeline.
type Engine struct {
	rules    []config.RuleConfig
	server   string
	detector *detection.RegexDetector
	logger   zerolog.Logger
	mu       sync.RWMutex

	// Callbacks for side effects (alerting, logging).
	onAlert func(ctx context.Context, alert AlertEvent)
	onEvent func(ctx context.Context, event EventRecord)
}

// AlertEvent carries information for the alert system.
type AlertEvent struct {
	Severity models.AlertSeverity `json:"severity"`
	Type     string               `json:"type"`
	Message  string               `json:"message"`
	RuleName string               `json:"rule_name"`
	EventID  string               `json:"event_id,omitempty"`
}

// EventRecord carries information for the event log.
type EventRecord struct {
	ServerID         string             `json:"server_id"`
	Direction        models.Direction   `json:"direction"`
	MessageType      string             `json:"message_type"`
	ToolName         string             `json:"tool_name,omitempty"`
	ToolArgs         json.RawMessage    `json:"tool_args,omitempty"`
	ActionTaken      models.ActionTaken `json:"action_taken"`
	RulesTriggered   []string           `json:"rules_triggered,omitempty"`
	DetectionResults map[string]string  `json:"detection_results,omitempty"`
	LatencyMS        int                `json:"latency_ms"`
}

// EngineOption configures the Engine.
type EngineOption func(*Engine)

// WithOnAlert sets the callback invoked when an alert should be raised.
func WithOnAlert(fn func(ctx context.Context, alert AlertEvent)) EngineOption {
	return func(e *Engine) { e.onAlert = fn }
}

// WithOnEvent sets the callback invoked when an event should be logged.
func WithOnEvent(fn func(ctx context.Context, event EventRecord)) EngineOption {
	return func(e *Engine) { e.onEvent = fn }
}

// NewEngine creates a new policy Engine for a specific server.
func NewEngine(server string, rules []config.RuleConfig, logger zerolog.Logger, opts ...EngineOption) *Engine {
	e := &Engine{
		rules:    rules,
		server:   server,
		detector: detection.NewRegexDetector(),
		logger:   logger,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// UpdateRules replaces the current rule set. Thread-safe.
func (e *Engine) UpdateRules(rules []config.RuleConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = rules
	e.logger.Info().Int("count", len(rules)).Msg("policy rules updated")
}

// Name returns the handler name for the pipeline.
func (e *Engine) Name() string { return "shield-policy-" + e.server }

// Priority returns 10 — policy runs early in the pipeline.
func (e *Engine) Priority() int { return 10 }

// Direction returns both — we inspect outgoing requests and incoming responses.
func (e *Engine) Direction() models.Direction { return models.DirectionBoth }

// HandleMessage evaluates the message against all rules and detection layers.
func (e *Engine) HandleMessage(ctx context.Context, msg *protocol.Message, mctx *proxy.MessageContext) (*proxy.Decision, error) {
	start := time.Now()
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	toolName, toolArgsMap := extractToolInfo(msg)
	var toolArgs json.RawMessage
	if toolArgsMap != nil {
		toolArgs, _ = json.Marshal(toolArgsMap)
	}
	var triggeredRules []string
	detectionResults := make(map[string]string)

	// Phase 1: Rule matching (outgoing requests).
	if mctx.Direction == models.DirectionOutgoing {
		for _, rule := range rules {
			if rule.ScanResponses || rule.ScanOutgoing {
				continue // These are scan rules, not match rules for outgoing.
			}
			result := MatchRule(msg, &rule)
			if result.Matched {
				triggeredRules = append(triggeredRules, rule.Name)
				decAction, shouldAlert := RuleActionToDecision(rule.Action)

				e.recordEvent(ctx, mctx, toolName, toolArgs, triggeredRules, detectionResults, decAction, start)

				if shouldAlert && e.onAlert != nil {
					e.onAlert(ctx, AlertEvent{
						Severity: models.AlertSeverityHigh,
						Type:     "policy_violation",
						Message:  fmt.Sprintf("Rule %q triggered on tool %q: %s", rule.Name, toolName, result.Reason),
						RuleName: rule.Name,
					})
				}

				switch decAction {
				case "block":
					return &proxy.Decision{
						Action: proxy.DecisionBlock,
						Reason: fmt.Sprintf("blocked by rule: %s", rule.Name),
						RuleID: rule.Name,
					}, nil
				case "redact":
					return &proxy.Decision{
						Action: proxy.DecisionRedact,
						Reason: fmt.Sprintf("redacted by rule: %s", rule.Name),
						RuleID: rule.Name,
					}, nil
				}
			}
		}
	}

	// Phase 2: Response scanning (incoming responses).
	if mctx.Direction == models.DirectionIncoming {
		// Run scan-based rules.
		for _, rule := range rules {
			if !rule.ScanResponses {
				continue
			}
			// Run regex detection on response fields.
			fields := ExtractResponseText(msg)
			for fieldName, fieldValue := range fields {
				hits := e.detector.Scan(fieldValue)
				for _, hit := range hits {
					detectionResults[fieldName] = hit.Category
					triggeredRules = append(triggeredRules, rule.Name)

					decAction, shouldAlert := RuleActionToDecision(rule.Action)
					e.recordEvent(ctx, mctx, toolName, nil, triggeredRules, detectionResults, decAction, start)

					if shouldAlert && e.onAlert != nil {
						e.onAlert(ctx, AlertEvent{
							Severity: models.AlertSeverityHigh,
							Type:     hit.Category,
							Message:  fmt.Sprintf("Detection in field %q: %s", fieldName, hit.Detail),
							RuleName: rule.Name,
						})
					}

					switch decAction {
					case "block":
						return &proxy.Decision{
							Action: proxy.DecisionBlock,
							Reason: fmt.Sprintf("detection: %s in %s (rule: %s)", hit.Category, fieldName, rule.Name),
							RuleID: rule.Name,
						}, nil
					case "redact":
						return &proxy.Decision{
							Action: proxy.DecisionRedact,
							Reason: fmt.Sprintf("redacted: %s in %s (rule: %s)", hit.Category, fieldName, rule.Name),
							RuleID: rule.Name,
						}, nil
					}
				}
			}
		}

		// Run detect-based rules (regex detection on specified categories).
		for _, rule := range rules {
			if len(rule.Detect.Values) == 0 {
				continue
			}
			fields := ExtractResponseText(msg)
			for fieldName, fieldValue := range fields {
				hits := e.detector.Scan(fieldValue)
				for _, hit := range hits {
					for _, wantCategory := range rule.Detect.Values {
						if hit.Category != wantCategory {
							continue
						}
						detectionResults[fieldName] = hit.Category
						triggeredRules = append(triggeredRules, rule.Name)

						decAction, shouldAlert := RuleActionToDecision(rule.Action)
						e.recordEvent(ctx, mctx, toolName, nil, triggeredRules, detectionResults, decAction, start)

						if shouldAlert && e.onAlert != nil {
							e.onAlert(ctx, AlertEvent{
								Severity: models.AlertSeverityMedium,
								Type:     hit.Category,
								Message:  fmt.Sprintf("Detected %q in field %q", wantCategory, fieldName),
								RuleName: rule.Name,
							})
						}

						if decAction == "block" {
							return &proxy.Decision{
								Action: proxy.DecisionBlock,
								Reason: fmt.Sprintf("detect %q in %s (rule: %s)", wantCategory, fieldName, rule.Name),
								RuleID: rule.Name,
							}, nil
						}
					}
				}
			}
		}
	}

	// Default: allow.
	e.recordEvent(ctx, mctx, toolName, toolArgs, nil, nil, "allow", start)
	return &proxy.Decision{Action: proxy.DecisionAllow}, nil
}

func (e *Engine) recordEvent(ctx context.Context, mctx *proxy.MessageContext, toolName string, toolArgs json.RawMessage, rules []string, detections map[string]string, action string, start time.Time) {
	if e.onEvent == nil {
		return
	}

	var actionTaken models.ActionTaken
	switch action {
	case "block":
		actionTaken = models.ActionBlocked
	case "redact":
		actionTaken = models.ActionRedacted
	case "allow":
		actionTaken = models.ActionAllowed
	default:
		actionTaken = models.ActionAllowed
	}

	e.onEvent(ctx, EventRecord{
		ServerID:         mctx.ServerName,
		Direction:        mctx.Direction,
		MessageType:      "jsonrpc",
		ToolName:         toolName,
		ToolArgs:         toolArgs,
		ActionTaken:      actionTaken,
		RulesTriggered:   rules,
		DetectionResults: detections,
		LatencyMS:        int(time.Since(start).Milliseconds()),
	})
}
