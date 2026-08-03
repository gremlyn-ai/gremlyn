package postgres

import (
	"context"
	"fmt"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventsRepo persists arena events to PostgreSQL.
type EventsRepo struct {
	pool *pgxpool.Pool
}

// NewEventsRepo creates an EventsRepo backed by the given connection pool.
func NewEventsRepo(pool *pgxpool.Pool) *EventsRepo {
	return &EventsRepo{pool: pool}
}

// InsertEvent stores a new arena event.
func (r *EventsRepo) InsertEvent(ctx context.Context, e models.ArenaEvent) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO arena_events (id, session_id, gremlin_type, gremlin_config,
			injected_at, agent_response, outcome, score, details)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.ID, e.SessionID, e.GremlinType, e.GremlinConfig,
		e.InjectedAt, e.AgentResponse, e.Outcome, e.Score, e.Details,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// ListBySession returns all events for a given session, ordered by injection time.
func (r *EventsRepo) ListBySession(ctx context.Context, sessionID string) ([]models.ArenaEvent, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, session_id, gremlin_type, gremlin_config,
			injected_at, agent_response, outcome, score, details
		FROM arena_events WHERE session_id = $1
		ORDER BY injected_at ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list events for session %q: %w", sessionID, err)
	}
	defer rows.Close()

	var events []models.ArenaEvent
	for rows.Next() {
		var e models.ArenaEvent
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.GremlinType, &e.GremlinConfig,
			&e.InjectedAt, &e.AgentResponse, &e.Outcome, &e.Score, &e.Details,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// CountByOutcome returns the number of events per outcome for a session.
func (r *EventsRepo) CountByOutcome(ctx context.Context, sessionID string) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT outcome, COUNT(*)
		FROM arena_events WHERE session_id = $1
		GROUP BY outcome`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("count by outcome: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var outcome string
		var count int
		if err := rows.Scan(&outcome, &count); err != nil {
			return nil, fmt.Errorf("scan outcome count: %w", err)
		}
		counts[outcome] = count
	}
	return counts, rows.Err()
}
