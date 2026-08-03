// Package api provides HTTP handlers and routing for the Shield REST API.
package api

import "github.com/gremlyn-ai/gremlyn/pkg/models"

// ErrorResponse is the standard JSON error format.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

// StatusResponse returns the shield service status.
type StatusResponse struct {
	Running bool     `json:"running"`
	Servers []string `json:"servers"`
	Uptime  string   `json:"uptime,omitempty"`
}

// EventListResponse wraps a list of events.
type EventListResponse struct {
	Events []models.Event `json:"events"`
	Total  int            `json:"total"`
}

// RuleListResponse wraps a list of rules.
type RuleListResponse struct {
	Rules []models.Rule `json:"rules"`
}

// AlertListResponse wraps a list of alerts.
type AlertListResponse struct {
	Alerts []models.Alert `json:"alerts"`
}

// MetricsResponse returns dashboard metrics.
type MetricsResponse struct {
	EventCounts map[string]int `json:"event_counts"`
	AlertCounts map[string]int `json:"alert_counts"`
	PeriodHours int            `json:"period_hours"`
}

// CreateRuleRequest is the body for creating a new rule.
type CreateRuleRequest struct {
	ServerID      string            `json:"server_id"`
	Name          string            `json:"name"`
	MatchTool     string            `json:"match_tool,omitempty"`
	ScanResponses bool              `json:"scan_responses,omitempty"`
	ScanOutgoing  bool              `json:"scan_outgoing,omitempty"`
	Detect        []string          `json:"detect,omitempty"`
	Action        models.RuleAction `json:"action"`
}

// UpdateRuleRequest is the body for updating a rule.
type UpdateRuleRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// UpdateAlertStatusRequest is the body for updating alert status.
type UpdateAlertStatusRequest struct {
	Status models.AlertStatus `json:"status"`
}

// ServerResponse is the API representation of a server.
type ServerResponse struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Mode        string            `json:"mode"`
	UpstreamURL string            `json:"upstream_url,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	AuthHeader  string            `json:"auth_header,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Status      string            `json:"status"`
	Source      string            `json:"source"`
	CreatedAt   string            `json:"created_at"`
}

// ServerListResponse wraps a list of servers.
type ServerListResponse struct {
	Servers []ServerResponse `json:"servers"`
}

// CreateServerRequest is the body for creating a new server.
type CreateServerRequest struct {
	Name        string            `json:"name"`
	Mode        string            `json:"mode"`
	UpstreamURL string            `json:"upstream_url,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	AuthHeader  string            `json:"auth_header,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// ExecRequest is the body for executing a CLI command via the web terminal.
type ExecRequest struct {
	Command string `json:"command"`
}

// ExecResponse is the response from a CLI command execution.
type ExecResponse struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"status"` // 0 = success, 1 = error
}

// UpdateServerRequest is the body for updating a server.
type UpdateServerRequest struct {
	Name        *string           `json:"name,omitempty"`
	Mode        *string           `json:"mode,omitempty"`
	UpstreamURL *string           `json:"upstream_url,omitempty"`
	Command     *string           `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	AuthHeader  *string           `json:"auth_header,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Status      *string           `json:"status,omitempty"`
}
