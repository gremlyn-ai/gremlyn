// Package models defines the shared domain types used across Gremlyn products
// (Shield, Arena, CLI). These types map to the database schema but are not ORM models.
package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// --- Enums ---

// ServerMode represents how the proxy connects to an MCP server.
type ServerMode string

const (
	// ServerModeWrap wraps a local stdio MCP server process.
	ServerModeWrap ServerMode = "wrap"
	// ServerModeProxy acts as a reverse proxy for HTTP/SSE MCP servers.
	ServerModeProxy ServerMode = "proxy"
	// ServerModeCloud routes through the Gremlyn cloud proxy.
	ServerModeCloud ServerMode = "cloud"
)

// Valid returns true if the ServerMode is a known value.
func (m ServerMode) Valid() bool {
	switch m {
	case ServerModeWrap, ServerModeProxy, ServerModeCloud:
		return true
	}
	return false
}

// ActionTaken represents the action taken on a proxied message.
type ActionTaken string

const (
	// ActionAllowed means the message was forwarded without modification.
	ActionAllowed ActionTaken = "allowed"
	// ActionBlocked means the message was rejected.
	ActionBlocked ActionTaken = "blocked"
	// ActionRedacted means sensitive data was removed before forwarding.
	ActionRedacted ActionTaken = "redacted"
	// ActionAlerted means the message was forwarded but an alert was raised.
	ActionAlerted ActionTaken = "alerted"
)

// Valid returns true if the ActionTaken is a known value.
func (a ActionTaken) Valid() bool {
	switch a {
	case ActionAllowed, ActionBlocked, ActionRedacted, ActionAlerted:
		return true
	}
	return false
}

// RuleAction represents the action a rule specifies when matched.
type RuleAction string

const (
	// RuleActionAllow lets the message through.
	RuleActionAllow RuleAction = "allow"
	// RuleActionBlock rejects the message.
	RuleActionBlock RuleAction = "block"
	// RuleActionBlockAndAlert rejects and sends an alert.
	RuleActionBlockAndAlert RuleAction = "block_and_alert"
	// RuleActionRedact sanitizes sensitive data but allows the message.
	RuleActionRedact RuleAction = "redact"
	// RuleActionRedactAndAlert sanitizes and sends an alert.
	RuleActionRedactAndAlert RuleAction = "redact_and_alert"
	// RuleActionThrottle rate-limits the message.
	RuleActionThrottle RuleAction = "throttle"
	// RuleActionPauseAndRequestApproval holds the message for human review.
	RuleActionPauseAndRequestApproval RuleAction = "pause_and_request_approval"
	// RuleActionLogOnly allows but flags for review.
	RuleActionLogOnly RuleAction = "log_only"
)

// Valid returns true if the RuleAction is a known value.
func (a RuleAction) Valid() bool {
	switch a {
	case RuleActionAllow, RuleActionBlock, RuleActionBlockAndAlert,
		RuleActionRedact, RuleActionRedactAndAlert, RuleActionThrottle,
		RuleActionPauseAndRequestApproval, RuleActionLogOnly:
		return true
	}
	return false
}

// AlertSeverity represents the severity level of an alert.
type AlertSeverity string

const (
	// AlertSeverityCritical is the highest severity.
	AlertSeverityCritical AlertSeverity = "critical"
	// AlertSeverityHigh indicates a serious security concern.
	AlertSeverityHigh AlertSeverity = "high"
	// AlertSeverityMedium indicates a moderate concern.
	AlertSeverityMedium AlertSeverity = "medium"
	// AlertSeverityLow indicates a minor concern.
	AlertSeverityLow AlertSeverity = "low"
)

// Valid returns true if the AlertSeverity is a known value.
func (s AlertSeverity) Valid() bool {
	switch s {
	case AlertSeverityCritical, AlertSeverityHigh, AlertSeverityMedium, AlertSeverityLow:
		return true
	}
	return false
}

// Direction represents the direction of a proxied message.
type Direction string

const (
	// DirectionOutgoing is from the MCP client to the MCP server.
	DirectionOutgoing Direction = "outgoing"
	// DirectionIncoming is from the MCP server to the MCP client.
	DirectionIncoming Direction = "incoming"
	// DirectionBoth matches both directions (used for handler registration).
	DirectionBoth Direction = "both"
)

// Valid returns true if the Direction is a known value.
func (d Direction) Valid() bool {
	switch d {
	case DirectionOutgoing, DirectionIncoming, DirectionBoth:
		return true
	}
	return false
}

// Matches returns true if this direction matches the given direction.
// DirectionBoth matches everything.
func (d Direction) Matches(other Direction) bool {
	return d == DirectionBoth || d == other
}

// SessionStatus represents the status of an Arena chaos session.
type SessionStatus string

const (
	// SessionStatusRunning means the session is currently active.
	SessionStatusRunning SessionStatus = "running"
	// SessionStatusCompleted means the session finished normally.
	SessionStatusCompleted SessionStatus = "completed"
	// SessionStatusCancelled means the session was stopped early.
	SessionStatusCancelled SessionStatus = "cancelled"
)

// Valid returns true if the SessionStatus is a known value.
func (s SessionStatus) Valid() bool {
	switch s {
	case SessionStatusRunning, SessionStatusCompleted, SessionStatusCancelled:
		return true
	}
	return false
}

// ArenaOutcome represents how the agent handled a gremlin injection.
type ArenaOutcome string

const (
	// OutcomeSurvived means the agent handled the injection correctly.
	OutcomeSurvived ArenaOutcome = "survived"
	// OutcomeCrashed means the agent failed or errored.
	OutcomeCrashed ArenaOutcome = "crashed"
	// OutcomeDegraded means the agent partially handled the injection.
	OutcomeDegraded ArenaOutcome = "degraded"
)

// Valid returns true if the ArenaOutcome is a known value.
func (o ArenaOutcome) Valid() bool {
	switch o {
	case OutcomeSurvived, OutcomeCrashed, OutcomeDegraded:
		return true
	}
	return false
}

// AlertStatus represents the current status of an alert.
type AlertStatus string

const (
	// AlertStatusNew means the alert has not been reviewed.
	AlertStatusNew AlertStatus = "new"
	// AlertStatusAcknowledged means someone has seen the alert.
	AlertStatusAcknowledged AlertStatus = "acknowledged"
	// AlertStatusResolved means the alert has been addressed.
	AlertStatusResolved AlertStatus = "resolved"
	// AlertStatusFalsePositive means the alert was a false positive.
	AlertStatusFalsePositive AlertStatus = "false_positive"
)

// Valid returns true if the AlertStatus is a known value.
func (s AlertStatus) Valid() bool {
	switch s {
	case AlertStatusNew, AlertStatusAcknowledged, AlertStatusResolved, AlertStatusFalsePositive:
		return true
	}
	return false
}

// RuleSource indicates how a rule was created.
type RuleSource string

const (
	// RuleSourceManual means the rule was written by hand.
	RuleSourceManual RuleSource = "manual"
	// RuleSourceTemplate means the rule came from an industry template.
	RuleSourceTemplate RuleSource = "template"
	// RuleSourceNaturalLanguage means the rule was generated from natural language.
	RuleSourceNaturalLanguage RuleSource = "natural_language"
)

// Valid returns true if the RuleSource is a known value.
func (s RuleSource) Valid() bool {
	switch s {
	case RuleSourceManual, RuleSourceTemplate, RuleSourceNaturalLanguage:
		return true
	}
	return false
}

// --- Domain Structs ---

// Server represents a registered MCP server in the Gremlyn platform.
type Server struct {
	ID            string            `json:"id" yaml:"id"`
	OrgID         string            `json:"org_id,omitempty" yaml:"-"`
	Name          string            `json:"name" yaml:"name"`
	Mode          ServerMode        `json:"mode" yaml:"mode"`
	UpstreamURL   string            `json:"upstream_url,omitempty" yaml:"upstream_url,omitempty"`
	Command       string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args          []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	AuthHeader    string            `json:"auth_header,omitempty" yaml:"auth_header,omitempty"`
	Headers       map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	ToolsSnapshot json.RawMessage   `json:"tools_snapshot,omitempty" yaml:"-"`
	ToolsHash     string            `json:"tools_hash,omitempty" yaml:"-"`
	Status        string            `json:"status" yaml:"-"`
	LastSeen      time.Time         `json:"last_seen,omitempty" yaml:"-"`
	CreatedAt     time.Time         `json:"created_at,omitempty" yaml:"-"`
}

// Rule represents a security rule in the Gremlyn policy engine.
type Rule struct {
	ID            string          `json:"id" yaml:"-"`
	OrgID         string          `json:"org_id,omitempty" yaml:"-"`
	ServerID      string          `json:"server_id,omitempty" yaml:"-"`
	Name          string          `json:"name" yaml:"name"`
	Match         *RuleMatch      `json:"match,omitempty" yaml:"match,omitempty"`
	ScanResponses bool            `json:"scan_responses,omitempty" yaml:"scan_responses,omitempty"`
	ScanOutgoing  bool            `json:"scan_outgoing,omitempty" yaml:"scan_outgoing,omitempty"`
	Detect        DetectConfig    `json:"detect,omitempty" yaml:"detect,omitempty"`
	EntityField   string          `json:"entity_field,omitempty" yaml:"entity_field,omitempty"`
	Action        RuleAction      `json:"action" yaml:"action"`
	Enabled       bool            `json:"enabled" yaml:"-"`
	Source        RuleSource      `json:"source,omitempty" yaml:"-"`
	OriginalText  string          `json:"original_text,omitempty" yaml:"-"`
	Config        json.RawMessage `json:"config,omitempty" yaml:"-"`
	CreatedAt     time.Time       `json:"created_at,omitempty" yaml:"-"`
	UpdatedAt     time.Time       `json:"updated_at,omitempty" yaml:"-"`
}

// RuleMatch defines the matching conditions for a rule.
type RuleMatch struct {
	Tool string                        `json:"tool,omitempty" yaml:"tool,omitempty"`
	Args map[string]RuleMatchCondition `json:"args,omitempty" yaml:"-"`
	Time *TimeCondition                `json:"time,omitempty" yaml:"time,omitempty"`
}

// RuleMatchCondition defines a single condition on a tool argument.
type RuleMatchCondition struct {
	GreaterThan   *float64 `json:"greater_than,omitempty" yaml:"greater_than,omitempty"`
	LessThan      *float64 `json:"less_than,omitempty" yaml:"less_than,omitempty"`
	Equals        any      `json:"equals,omitempty" yaml:"equals,omitempty"`
	MustStartWith string   `json:"must_start_with,omitempty" yaml:"must_start_with,omitempty"`
	NotContains   []string `json:"not_contains,omitempty" yaml:"not_contains,omitempty"`
	Contains      []string `json:"contains,omitempty" yaml:"contains,omitempty"`
	Regex         string   `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// TimeCondition defines a time-based rule condition.
type TimeCondition struct {
	Outside  string `json:"outside,omitempty" yaml:"outside,omitempty"`
	Timezone string `json:"timezone,omitempty" yaml:"timezone,omitempty"`
}

// DetectConfig can be either a single string or a list of strings.
// It normalizes both forms into a slice.
type DetectConfig struct {
	Values []string
}

// MarshalJSON implements json.Marshaler for DetectConfig.
func (d DetectConfig) MarshalJSON() ([]byte, error) {
	if len(d.Values) == 0 {
		return []byte("null"), nil
	}
	if len(d.Values) == 1 {
		return json.Marshal(d.Values[0])
	}
	return json.Marshal(d.Values)
}

// UnmarshalJSON implements json.Unmarshaler for DetectConfig.
func (d *DetectConfig) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		d.Values = nil
		return nil
	}

	// Try single string.
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		d.Values = []string{single}
		return nil
	}

	// Try string slice.
	var slice []string
	if err := json.Unmarshal(data, &slice); err == nil {
		d.Values = slice
		return nil
	}

	return fmt.Errorf("detect config must be a string or string array, got: %s", string(data))
}

// UnmarshalYAML implements yaml.Unmarshaler for DetectConfig.
func (d *DetectConfig) UnmarshalYAML(unmarshal func(any) error) error {
	// Try single string.
	var single string
	if err := unmarshal(&single); err == nil {
		d.Values = []string{single}
		return nil
	}

	// Try string slice.
	var slice []string
	if err := unmarshal(&slice); err == nil {
		d.Values = slice
		return nil
	}

	return fmt.Errorf("detect config must be a string or string array")
}

// Event represents a single proxied MCP message and its analysis results.
type Event struct {
	ID               string            `json:"id"`
	OrgID            string            `json:"org_id,omitempty"`
	ServerID         string            `json:"server_id"`
	SessionID        string            `json:"session_id,omitempty"`
	Timestamp        time.Time         `json:"timestamp"`
	Direction        Direction         `json:"direction"`
	MessageType      string            `json:"message_type"`
	ToolName         string            `json:"tool_name,omitempty"`
	ToolArgs         json.RawMessage   `json:"tool_args,omitempty"`
	ResponsePayload  json.RawMessage   `json:"response_payload,omitempty"`
	PayloadRef       string            `json:"payload_ref,omitempty"`
	ActionTaken      ActionTaken       `json:"action_taken"`
	RulesTriggered   []string          `json:"rules_triggered,omitempty"`
	DetectionResults map[string]string `json:"detection_results,omitempty"`
	LatencyMS        int               `json:"latency_ms"`
	CreatedAt        time.Time         `json:"created_at,omitempty"`
}

// Alert represents a security alert triggered by the detection engine.
type Alert struct {
	ID               string        `json:"id"`
	OrgID            string        `json:"org_id,omitempty"`
	EventID          string        `json:"event_id"`
	Severity         AlertSeverity `json:"severity"`
	Type             string        `json:"type"`
	Message          string        `json:"message"`
	Status           AlertStatus   `json:"status"`
	NotifiedChannels []string      `json:"notified_channels,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	ResolvedAt       *time.Time    `json:"resolved_at,omitempty"`
}

// ArenaSession represents a chaos testing session.
type ArenaSession struct {
	ID               string          `json:"id"`
	OrgID            string          `json:"org_id,omitempty"`
	ServerID         string          `json:"server_id"`
	Status           SessionStatus   `json:"status"`
	Config           json.RawMessage `json:"config"`
	Results          json.RawMessage `json:"results,omitempty"`
	GremlinsSent     int             `json:"gremlins_sent"`
	GremlinsSurvived int             `json:"gremlins_survived"`
	GremlinsCrashed  int             `json:"gremlins_crashed"`
	StartedAt        time.Time       `json:"started_at"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
}

// ArenaEvent represents a single gremlin injection and its result within a chaos session.
type ArenaEvent struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	GremlinType   string          `json:"gremlin_type"`
	GremlinConfig json.RawMessage `json:"gremlin_config"`
	InjectedAt    time.Time       `json:"injected_at"`
	AgentResponse json.RawMessage `json:"agent_response,omitempty"`
	Outcome       ArenaOutcome    `json:"outcome"`
	Score         int             `json:"score"`
	Details       json.RawMessage `json:"details,omitempty"`
}

// BehavioralProfile represents a behavioral fingerprint for an agent-server pair.
type BehavioralProfile struct {
	ID          string          `json:"id"`
	OrgID       string          `json:"org_id,omitempty"`
	ServerID    string          `json:"server_id"`
	ProfileData json.RawMessage `json:"profile_data"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// ToolSnapshot captures MCP server tool definitions at a point in time for rug pull detection.
type ToolSnapshot struct {
	ID              string          `json:"id"`
	ServerID        string          `json:"server_id"`
	ToolsDefinition json.RawMessage `json:"tools_definition"`
	ToolsHash       string          `json:"tools_hash"`
	CapturedAt      time.Time       `json:"captured_at"`
}

// --- Custom Error Types ---

// ErrPolicyViolation indicates a message was blocked by a policy rule.
type ErrPolicyViolation struct {
	Rule    string     `json:"rule"`
	Message string     `json:"message"`
	Action  RuleAction `json:"action"`
}

func (e *ErrPolicyViolation) Error() string {
	return fmt.Sprintf("policy violation [%s]: %s (action: %s)", e.Rule, e.Message, e.Action)
}

// ErrProxyTimeout indicates a proxy operation timed out.
type ErrProxyTimeout struct {
	Duration   time.Duration `json:"duration"`
	ServerName string        `json:"server_name"`
}

func (e *ErrProxyTimeout) Error() string {
	return fmt.Sprintf("proxy timeout for server %q after %s", e.ServerName, e.Duration)
}

// ErrInvalidMessage indicates a message could not be parsed.
type ErrInvalidMessage struct {
	Reason   string `json:"reason"`
	RawBytes []byte `json:"-"`
}

func (e *ErrInvalidMessage) Error() string {
	return fmt.Sprintf("invalid message: %s", e.Reason)
}

// ErrTransportClosed is a sentinel error for when a transport has been shut down.
var ErrTransportClosed = fmt.Errorf("transport closed")
