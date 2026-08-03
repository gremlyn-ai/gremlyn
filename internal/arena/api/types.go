// Package api provides the REST and WebSocket API for Gremlyn Arena.
package api

import (
	"encoding/json"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

// ── Request types ──

// CreateSessionRequest is the body for POST /arena/sessions.
type CreateSessionRequest struct {
	ServerID  string   `json:"server_id"`
	Gremlins  []string `json:"gremlins"`
	Intensity string   `json:"intensity"`
	Prompts   []string `json:"prompts,omitempty"`
}

// ── Response types ──

// SessionResponse is the API representation of a session.
type SessionResponse struct {
	ID               string          `json:"id"`
	ServerID         string          `json:"server_id"`
	Status           string          `json:"status"`
	Config           json.RawMessage `json:"config"`
	Results          json.RawMessage `json:"results,omitempty"`
	GremlinsSent     int             `json:"gremlins_sent"`
	GremlinsSurvived int             `json:"gremlins_survived"`
	GremlinsCrashed  int             `json:"gremlins_crashed"`
	StartedAt        string          `json:"started_at"`
	CompletedAt      *string         `json:"completed_at,omitempty"`
}

// SessionListResponse wraps a list of sessions.
type SessionListResponse struct {
	Sessions []SessionResponse `json:"sessions"`
}

// GremlinInfo describes an available gremlin.
type GremlinInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GremlinListResponse wraps a list of gremlins.
type GremlinListResponse struct {
	Gremlins []GremlinInfo `json:"gremlins"`
}

// EventResponse is the API representation of an arena event.
type EventResponse struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	GremlinType   string          `json:"gremlin_type"`
	GremlinConfig json.RawMessage `json:"gremlin_config"`
	InjectedAt    string          `json:"injected_at"`
	AgentResponse json.RawMessage `json:"agent_response,omitempty"`
	Outcome       string          `json:"outcome"`
	Score         int             `json:"score"`
	Details       json.RawMessage `json:"details,omitempty"`
}

// EventListResponse wraps a list of events.
type EventListResponse struct {
	Events []EventResponse `json:"events"`
}

// StatusResponse is the health/status response.
type StatusResponse struct {
	Status         string `json:"status"`
	ActiveSessions int    `json:"active_sessions"`
	Gremlins       int    `json:"gremlins_available"`
}

// ErrorResponse is a standard error response.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// WSMessage is a WebSocket message with a type field for routing.
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// ── Helpers ──

func sessionToResponse(s models.ArenaSession) SessionResponse {
	resp := SessionResponse{
		ID:               s.ID,
		ServerID:         s.ServerID,
		Status:           string(s.Status),
		Config:           s.Config,
		Results:          s.Results,
		GremlinsSent:     s.GremlinsSent,
		GremlinsSurvived: s.GremlinsSurvived,
		GremlinsCrashed:  s.GremlinsCrashed,
		StartedAt:        s.StartedAt.Format("2006-01-02T15:04:05Z"),
	}
	if s.CompletedAt != nil {
		t := s.CompletedAt.Format("2006-01-02T15:04:05Z")
		resp.CompletedAt = &t
	}
	return resp
}

func eventToResponse(e models.ArenaEvent) EventResponse {
	return EventResponse{
		ID:            e.ID,
		SessionID:     e.SessionID,
		GremlinType:   e.GremlinType,
		GremlinConfig: e.GremlinConfig,
		InjectedAt:    e.InjectedAt.Format("2006-01-02T15:04:05Z"),
		AgentResponse: e.AgentResponse,
		Outcome:       string(e.Outcome),
		Score:         e.Score,
		Details:       e.Details,
	}
}
