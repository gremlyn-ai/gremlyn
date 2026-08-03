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

// ServersRepo implements service.ServerStore backed by SQLite.
type ServersRepo struct {
	db *sql.DB
}

// Insert creates a new server and returns its generated ID.
func (r *ServersRepo) Insert(ctx context.Context, s *models.Server) (string, error) {
	id := uuid.New().String()
	now := formatTime(time.Now())

	argsJSON, err := json.Marshal(s.Args)
	if err != nil {
		return "", fmt.Errorf("marshaling args: %w", err)
	}
	envJSON, err := json.Marshal(s.Env)
	if err != nil {
		return "", fmt.Errorf("marshaling env: %w", err)
	}

	headersJSON, err := json.Marshal(s.Headers)
	if err != nil {
		return "", fmt.Errorf("marshaling headers: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO servers (id, name, mode, upstream_url, command, args, env, auth_header, headers, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, s.Name, s.Mode, s.UpstreamURL, s.Command, string(argsJSON), string(envJSON),
		s.AuthHeader, string(headersJSON), "inactive", now, now,
	)
	if err != nil {
		return "", fmt.Errorf("inserting server: %w", err)
	}
	return id, nil
}

// GetByID retrieves a server by ID.
func (r *ServersRepo) GetByID(ctx context.Context, id string) (*models.Server, error) {
	return r.scanRow(r.db.QueryRowContext(ctx, `
		SELECT id, name, mode, upstream_url, command, args, env, auth_header, headers, status, last_seen, created_at, updated_at
		FROM servers WHERE id = ?`, id))
}

// GetByName retrieves a server by name.
func (r *ServersRepo) GetByName(ctx context.Context, name string) (*models.Server, error) {
	return r.scanRow(r.db.QueryRowContext(ctx, `
		SELECT id, name, mode, upstream_url, command, args, env, auth_header, headers, status, last_seen, created_at, updated_at
		FROM servers WHERE name = ?`, name))
}

// List returns all servers.
func (r *ServersRepo) List(ctx context.Context) ([]models.Server, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, mode, upstream_url, command, args, env, auth_header, headers, status, last_seen, created_at, updated_at
		FROM servers ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing servers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var servers []models.Server
	for rows.Next() {
		s, err := r.scanRows(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, *s)
	}
	return servers, rows.Err()
}

// Update modifies an existing server.
func (r *ServersRepo) Update(ctx context.Context, s *models.Server) error {
	argsJSON, err := json.Marshal(s.Args)
	if err != nil {
		return fmt.Errorf("marshaling args: %w", err)
	}
	envJSON, err := json.Marshal(s.Env)
	if err != nil {
		return fmt.Errorf("marshaling env: %w", err)
	}

	headersJSON, err := json.Marshal(s.Headers)
	if err != nil {
		return fmt.Errorf("marshaling headers: %w", err)
	}

	now := formatTime(time.Now())
	res, err := r.db.ExecContext(ctx, `
		UPDATE servers SET name=?, mode=?, upstream_url=?, command=?, args=?, env=?, auth_header=?, headers=?, status=?, updated_at=?
		WHERE id=?`,
		s.Name, s.Mode, s.UpstreamURL, s.Command, string(argsJSON), string(envJSON),
		s.AuthHeader, string(headersJSON), s.Status, now, s.ID,
	)
	if err != nil {
		return fmt.Errorf("updating server: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("server %q not found", s.ID)
	}
	return nil
}

// Delete removes a server by ID.
func (r *ServersRepo) Delete(ctx context.Context, id string) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting server: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("server %q not found", id)
	}
	return nil
}

func (r *ServersRepo) scanRow(row *sql.Row) (*models.Server, error) {
	var s models.Server
	var upstreamURL, command sql.NullString
	var argsJSON, envJSON, headersJSON string
	var lastSeen *string
	var createdAt, updatedAt string

	err := row.Scan(
		&s.ID, &s.Name, &s.Mode, &upstreamURL, &command,
		&argsJSON, &envJSON, &s.AuthHeader, &headersJSON, &s.Status, &lastSeen,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning server: %w", err)
	}
	return r.hydrate(&s, upstreamURL, command, argsJSON, envJSON, headersJSON, lastSeen, createdAt)
}

func (r *ServersRepo) scanRows(rows *sql.Rows) (*models.Server, error) {
	var s models.Server
	var upstreamURL, command sql.NullString
	var argsJSON, envJSON, headersJSON string
	var lastSeen *string
	var createdAt, updatedAt string

	err := rows.Scan(
		&s.ID, &s.Name, &s.Mode, &upstreamURL, &command,
		&argsJSON, &envJSON, &s.AuthHeader, &headersJSON, &s.Status, &lastSeen,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scanning server row: %w", err)
	}
	return r.hydrate(&s, upstreamURL, command, argsJSON, envJSON, headersJSON, lastSeen, createdAt)
}

func (r *ServersRepo) hydrate(
	s *models.Server,
	upstreamURL, command sql.NullString,
	argsJSON, envJSON, headersJSON string,
	lastSeen *string,
	createdAt string,
) (*models.Server, error) {
	s.UpstreamURL = upstreamURL.String
	s.Command = command.String

	if err := json.Unmarshal([]byte(argsJSON), &s.Args); err != nil {
		s.Args = nil
	}
	if err := json.Unmarshal([]byte(envJSON), &s.Env); err != nil {
		s.Env = nil
	}
	if err := json.Unmarshal([]byte(headersJSON), &s.Headers); err != nil {
		s.Headers = nil
	}

	if lastSeen != nil && *lastSeen != "" {
		t, err := parseTime(*lastSeen)
		if err == nil {
			s.LastSeen = t
		}
	}

	t, err := parseTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	s.CreatedAt = t

	return s, nil
}
