package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

// EventsRepo implements session.EventStore backed by SQLite.
type EventsRepo struct {
	db *sql.DB
}

// InsertEvent stores a new arena event.
func (r *EventsRepo) InsertEvent(ctx context.Context, e models.ArenaEvent) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO arena_events (id, session_id, gremlin_type, gremlin_config,
			injected_at, agent_response, outcome, score, details)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		e.ID, e.SessionID, e.GremlinType,
		jsonOrNull(e.GremlinConfig), formatTime(e.InjectedAt),
		jsonOrNull(e.AgentResponse), e.Outcome, e.Score,
		jsonOrNull(e.Details),
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// ListBySession returns all events for a given session, ordered by injection time.
func (r *EventsRepo) ListBySession(ctx context.Context, sessionID string) ([]models.ArenaEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, session_id, gremlin_type, gremlin_config,
			injected_at, agent_response, outcome, score, details
		FROM arena_events WHERE session_id = ?
		ORDER BY injected_at ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list events for session %q: %w", sessionID, err)
	}
	defer func() { _ = rows.Close() }()

	var events []models.ArenaEvent
	for rows.Next() {
		var e models.ArenaEvent
		var gremlinConfig, agentResponse, details sql.NullString
		var injectedAt string

		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.GremlinType, &gremlinConfig,
			&injectedAt, &agentResponse, &e.Outcome, &e.Score, &details,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		if gremlinConfig.Valid {
			e.GremlinConfig = json.RawMessage(gremlinConfig.String)
		}
		if agentResponse.Valid {
			e.AgentResponse = json.RawMessage(agentResponse.String)
		}
		if details.Valid {
			e.Details = json.RawMessage(details.String)
		}

		t, err := parseTime(injectedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing injected_at: %w", err)
		}
		e.InjectedAt = t

		events = append(events, e)
	}
	return events, rows.Err()
}
