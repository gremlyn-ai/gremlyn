package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

// SessionsRepo implements session.Store backed by SQLite.
type SessionsRepo struct {
	db *sql.DB
}

// InsertSession stores a new session.
func (r *SessionsRepo) InsertSession(ctx context.Context, s models.ArenaSession) error {
	var completedAt interface{}
	if s.CompletedAt != nil {
		completedAt = formatTime(*s.CompletedAt)
	}

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO arena_sessions (id, server_id, status, config, results,
			gremlins_sent, gremlins_survived, gremlins_crashed, started_at, completed_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.ServerID, s.Status,
		jsonOrNull(s.Config), jsonOrNull(s.Results),
		s.GremlinsSent, s.GremlinsSurvived, s.GremlinsCrashed,
		formatTime(s.StartedAt), completedAt,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// UpdateSession updates an existing session's mutable fields.
func (r *SessionsRepo) UpdateSession(ctx context.Context, s models.ArenaSession) error {
	var completedAt interface{}
	if s.CompletedAt != nil {
		completedAt = formatTime(*s.CompletedAt)
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE arena_sessions
		SET status = ?, results = ?, gremlins_sent = ?,
			gremlins_survived = ?, gremlins_crashed = ?, completed_at = ?
		WHERE id = ?`,
		s.Status, jsonOrNull(s.Results), s.GremlinsSent,
		s.GremlinsSurvived, s.GremlinsCrashed, completedAt, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	return nil
}

// GetSession retrieves a session by ID.
func (r *SessionsRepo) GetSession(ctx context.Context, id string) (models.ArenaSession, error) {
	var s models.ArenaSession
	var configStr, resultsStr sql.NullString
	var startedAt string
	var completedAt sql.NullString

	err := r.db.QueryRowContext(ctx, `
		SELECT id, server_id, status, config, results,
			gremlins_sent, gremlins_survived, gremlins_crashed, started_at, completed_at
		FROM arena_sessions WHERE id = ?`, id,
	).Scan(
		&s.ID, &s.ServerID, &s.Status, &configStr, &resultsStr,
		&s.GremlinsSent, &s.GremlinsSurvived, &s.GremlinsCrashed,
		&startedAt, &completedAt,
	)
	if err != nil {
		return s, fmt.Errorf("get session %q: %w", id, err)
	}

	if configStr.Valid {
		s.Config = json.RawMessage(configStr.String)
	}
	if resultsStr.Valid {
		s.Results = json.RawMessage(resultsStr.String)
	}

	t, err := parseTime(startedAt)
	if err != nil {
		return s, fmt.Errorf("parsing started_at: %w", err)
	}
	s.StartedAt = t

	completed, err := parseNullableTime(nullStringPtr(completedAt))
	if err != nil {
		return s, fmt.Errorf("parsing completed_at: %w", err)
	}
	s.CompletedAt = completed

	return s, nil
}

// ListSessions returns all sessions ordered by start time (newest first).
func (r *SessionsRepo) ListSessions(ctx context.Context) ([]models.ArenaSession, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_id, status, config, results,
			gremlins_sent, gremlins_survived, gremlins_crashed, started_at, completed_at
		FROM arena_sessions ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []models.ArenaSession
	for rows.Next() {
		var s models.ArenaSession
		var configStr, resultsStr sql.NullString
		var startedAt string
		var completedAt sql.NullString

		if err := rows.Scan(
			&s.ID, &s.ServerID, &s.Status, &configStr, &resultsStr,
			&s.GremlinsSent, &s.GremlinsSurvived, &s.GremlinsCrashed,
			&startedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}

		if configStr.Valid {
			s.Config = json.RawMessage(configStr.String)
		}
		if resultsStr.Valid {
			s.Results = json.RawMessage(resultsStr.String)
		}

		t, err := parseTime(startedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing started_at: %w", err)
		}
		s.StartedAt = t

		completed, err := parseNullableTime(nullStringPtr(completedAt))
		if err != nil {
			return nil, fmt.Errorf("parsing completed_at: %w", err)
		}
		s.CompletedAt = completed

		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}
