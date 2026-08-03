package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// EventsRepo provides CRUD operations for shield events.
type EventsRepo struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
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

	var id string
	err = r.pool.QueryRow(ctx, `
		INSERT INTO events (
			server_id, session_id, direction, message_type, tool_name,
			tool_args, response_payload, payload_ref, action_taken,
			rules_triggered, detection_results, latency_ms, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id`,
		e.ServerID, e.SessionID, e.Direction, e.MessageType, e.ToolName,
		e.ToolArgs, e.ResponsePayload, e.PayloadRef, e.ActionTaken,
		rulesJSON, detectJSON, e.LatencyMS, time.Now(),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("inserting event: %w", err)
	}
	return id, nil
}

// ListByServer returns events for a server ordered by timestamp descending.
func (r *EventsRepo) ListByServer(ctx context.Context, serverID string, limit, offset int) ([]models.Event, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, session_id, direction, message_type, tool_name,
			   tool_args, response_payload, payload_ref, action_taken,
			   rules_triggered, detection_results, latency_ms, created_at
		FROM events
		WHERE server_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`,
		serverID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// ListBlocked returns only blocked events across all servers.
func (r *EventsRepo) ListBlocked(ctx context.Context, limit, offset int) ([]models.Event, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, session_id, direction, message_type, tool_name,
			   tool_args, response_payload, payload_ref, action_taken,
			   rules_triggered, detection_results, latency_ms, created_at
		FROM events
		WHERE action_taken = 'blocked'
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("querying blocked events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

// CountByAction returns event counts grouped by action_taken for the last N hours.
func (r *EventsRepo) CountByAction(ctx context.Context, hours int) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT action_taken, COUNT(*)
		FROM events
		WHERE created_at > NOW() - INTERVAL '1 hour' * $1
		GROUP BY action_taken`,
		hours,
	)
	if err != nil {
		return nil, fmt.Errorf("counting events by action: %w", err)
	}
	defer rows.Close()

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

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanEvents(rows rowScanner) ([]models.Event, error) {
	events := make([]models.Event, 0)
	for rows.Next() {
		var e models.Event
		var rulesJSON, detectJSON []byte

		if err := rows.Scan(
			&e.ID, &e.ServerID, &e.SessionID, &e.Direction, &e.MessageType,
			&e.ToolName, &e.ToolArgs, &e.ResponsePayload, &e.PayloadRef,
			&e.ActionTaken, &rulesJSON, &detectJSON, &e.LatencyMS, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}

		if rulesJSON != nil {
			if err := json.Unmarshal(rulesJSON, &e.RulesTriggered); err != nil {
				return nil, fmt.Errorf("unmarshaling rules triggered: %w", err)
			}
		}
		if detectJSON != nil {
			if err := json.Unmarshal(detectJSON, &e.DetectionResults); err != nil {
				return nil, fmt.Errorf("unmarshaling detection results: %w", err)
			}
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
