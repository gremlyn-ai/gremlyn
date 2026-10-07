package models

import (
	"encoding/json"
	"fmt"
	"time"
)

type Direction string

const (
	DirectionOutgoing Direction = "outgoing"
	DirectionIncoming Direction = "incoming"
	DirectionBoth     Direction = "both"
)

func (d Direction) Valid() bool {
	switch d {
	case DirectionOutgoing, DirectionIncoming, DirectionBoth:
		return true
	}
	return false
}

func (d Direction) Matches(other Direction) bool {
	return d == DirectionBoth || d == other
}

type SessionStatus string

const (
	SessionStatusRunning   SessionStatus = "running"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
)

func (s SessionStatus) Valid() bool {
	switch s {
	case SessionStatusRunning, SessionStatusCompleted, SessionStatusFailed:
		return true
	}
	return false
}

type ArenaOutcome string

const (
	OutcomeSurvived   ArenaOutcome = "survived"
	OutcomeCrashed    ArenaOutcome = "crashed"
	OutcomeDegraded   ArenaOutcome = "degraded"
	OutcomeUnmeasured ArenaOutcome = "unmeasured"
)

func (o ArenaOutcome) Valid() bool {
	switch o {
	case OutcomeSurvived, OutcomeCrashed, OutcomeDegraded, OutcomeUnmeasured:
		return true
	}
	return false
}

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

var ErrTransportClosed = fmt.Errorf("transport closed")
