// Package postgres provides PostgreSQL-backed repositories for Gremlyn Arena.
package postgres

import (
	"context"
	"fmt"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionsRepo persists arena sessions to PostgreSQL.
type SessionsRepo struct {
	pool *pgxpool.Pool
}

// NewSessionsRepo creates a SessionsRepo backed by the given connection pool.
func NewSessionsRepo(pool *pgxpool.Pool) *SessionsRepo {
	return &SessionsRepo{pool: pool}
}

// InsertSession stores a new session.
func (r *SessionsRepo) InsertSession(ctx context.Context, s models.ArenaSession) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO arena_sessions (id, server_id, status, config, results,
			gremlins_sent, gremlins_survived, gremlins_crashed, started_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		s.ID, s.ServerID, s.Status, s.Config, s.Results,
		s.GremlinsSent, s.GremlinsSurvived, s.GremlinsCrashed,
		s.StartedAt, s.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// UpdateSession updates an existing session's mutable fields.
func (r *SessionsRepo) UpdateSession(ctx context.Context, s models.ArenaSession) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE arena_sessions
		SET status = $2, results = $3, gremlins_sent = $4,
			gremlins_survived = $5, gremlins_crashed = $6, completed_at = $7
		WHERE id = $1`,
		s.ID, s.Status, s.Results, s.GremlinsSent,
		s.GremlinsSurvived, s.GremlinsCrashed, s.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

// GetSession retrieves a session by ID.
func (r *SessionsRepo) GetSession(ctx context.Context, id string) (models.ArenaSession, error) {
	var s models.ArenaSession
	err := r.pool.QueryRow(ctx, `
		SELECT id, server_id, status, config, results,
			gremlins_sent, gremlins_survived, gremlins_crashed, started_at, completed_at
		FROM arena_sessions WHERE id = $1`, id,
	).Scan(
		&s.ID, &s.ServerID, &s.Status, &s.Config, &s.Results,
		&s.GremlinsSent, &s.GremlinsSurvived, &s.GremlinsCrashed,
		&s.StartedAt, &s.CompletedAt,
	)
	if err != nil {
		return s, fmt.Errorf("get session %q: %w", id, err)
	}
	return s, nil
}

// ListSessions returns all sessions ordered by start time (newest first).
func (r *SessionsRepo) ListSessions(ctx context.Context) ([]models.ArenaSession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, status, config, results,
			gremlins_sent, gremlins_survived, gremlins_crashed, started_at, completed_at
		FROM arena_sessions ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []models.ArenaSession
	for rows.Next() {
		var s models.ArenaSession
		if err := rows.Scan(
			&s.ID, &s.ServerID, &s.Status, &s.Config, &s.Results,
			&s.GremlinsSent, &s.GremlinsSurvived, &s.GremlinsCrashed,
			&s.StartedAt, &s.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
