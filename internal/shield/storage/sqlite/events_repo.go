package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

// EventsRepo implements service.EventStore backed by SQLite.
type EventsRepo struct {
	db *sql.DB
}

// Insert persists a new event and returns the generated ID.
func (r *EventsRepo) Insert(ctx context.Context, e *models.Event) (string, error) {
	rulesJSON, err := json.Marshal(e.RulesTriggered)
	if err != nil {
		return "", fmt.Errorf("marshaling rules triggered: %w", err)
	}
	detectJSON, err := json.Marshal(e.DetectionResults)
	if err != nil {
		return "", fmt.Errorf("marshaling detection results: %w", err)
	}

	id := uuid.New().String()
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO events (
			id, server_id, session_id, direction, message_type, tool_name,
			tool_args, response_payload, payload_ref, action_taken,
			rules_triggered, detection_results, latency_ms, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, e.ServerID, e.SessionID, e.Direction, e.MessageType, e.ToolName,
		jsonOrNull(e.ToolArgs), jsonOrNull(e.ResponsePayload), e.PayloadRef, e.ActionTaken,
		string(rulesJSON), string(detectJSON), e.LatencyMS, formatTime(time.Now()),
	)
	if err != nil {
		return "", fmt.Errorf("inserting event: %w", err)
	}
	return id, nil
}

// ListByServer returns events for a server ordered by timestamp descending.
func (r *EventsRepo) ListByServer(ctx context.Context, serverID string, limit, offset int) ([]models.Event, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_id, session_id, direction, message_type, tool_name,
			   tool_args, response_payload, payload_ref, action_taken,
			   rules_triggered, detection_results, latency_ms, created_at
		FROM events
		WHERE server_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`,
		serverID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEvents(rows)
}

// ListBlocked returns only blocked events across all servers.
func (r *EventsRepo) ListBlocked(ctx context.Context, limit, offset int) ([]models.Event, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_id, session_id, direction, message_type, tool_name,
			   tool_args, response_payload, payload_ref, action_taken,
			   rules_triggered, detection_results, latency_ms, created_at
		FROM events
		WHERE action_taken = 'blocked'
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("querying blocked events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEvents(rows)
}

// CountByAction returns event counts grouped by action_taken for the last N hours.
func (r *EventsRepo) CountByAction(ctx context.Context, hours int) (map[string]int, error) {
	cutoff := formatTime(time.Now().Add(-time.Duration(hours) * time.Hour))
	rows, err := r.db.QueryContext(ctx, `
		SELECT action_taken, COUNT(*)
		FROM events
		WHERE created_at > ?
		GROUP BY action_taken`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("counting events by action: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var action string
		var count int
		if err := rows.Scan(&action, &count); err != nil {
			return nil, fmt.Errorf("scanning event count: %w", err)
		}
		counts[action] = count
	}
	return counts, rows.Err()
}

func scanEvents(rows *sql.Rows) ([]models.Event, error) {
	events := make([]models.Event, 0)
	for rows.Next() {
		var e models.Event
		var toolArgs, responsePayload, rulesJSON, detectJSON sql.NullString
		var createdAt string

		if err := rows.Scan(
			&e.ID, &e.ServerID, &e.SessionID, &e.Direction, &e.MessageType,
			&e.ToolName, &toolArgs, &responsePayload, &e.PayloadRef,
			&e.ActionTaken, &rulesJSON, &detectJSON, &e.LatencyMS, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}

		if toolArgs.Valid {
			e.ToolArgs = json.RawMessage(toolArgs.String)
		}
		if responsePayload.Valid {
			e.ResponsePayload = json.RawMessage(responsePayload.String)
		}
		if rulesJSON.Valid {
			if err := json.Unmarshal([]byte(rulesJSON.String), &e.RulesTriggered); err != nil {
				return nil, fmt.Errorf("unmarshaling rules triggered: %w", err)
			}
		}
		if detectJSON.Valid {
			if err := json.Unmarshal([]byte(detectJSON.String), &e.DetectionResults); err != nil {
				return nil, fmt.Errorf("unmarshaling detection results: %w", err)
			}
		}

		t, err := parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parsing created_at: %w", err)
		}
		e.CreatedAt = t

		events = append(events, e)
	}
	return events, rows.Err()
}

// jsonOrNull returns the string representation of a json.RawMessage, or nil if empty.
func jsonOrNull(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
