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

// ServersRepo provides CRUD operations for MCP server configurations.
type ServersRepo struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// Insert creates a new server and returns its generated ID.
func (r *ServersRepo) Insert(ctx context.Context, s *models.Server) (string, error) {
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

	var id string
	now := time.Now()
	err = r.pool.QueryRow(ctx, `
		INSERT INTO servers (name, mode, upstream_url, command, args, env, auth_header, headers, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id`,
		s.Name, s.Mode, s.UpstreamURL, s.Command, argsJSON, envJSON,
		s.AuthHeader, headersJSON, "inactive", now, now,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("inserting server: %w", err)
	}
	return id, nil
}

// GetByID retrieves a server by ID.
func (r *ServersRepo) GetByID(ctx context.Context, id string) (*models.Server, error) {
	var s models.Server
	var argsJSON, envJSON, headersJSON []byte
	var updatedAt time.Time

	err := r.pool.QueryRow(ctx, `
		SELECT id, name, mode, upstream_url, command, args, env, auth_header, headers, status, last_seen, created_at, updated_at
		FROM servers WHERE id = $1`, id,
	).Scan(
		&s.ID, &s.Name, &s.Mode, &s.UpstreamURL, &s.Command,
		&argsJSON, &envJSON, &s.AuthHeader, &headersJSON, &s.Status, &s.LastSeen,
		&s.CreatedAt, &updatedAt,
	)
	_ = updatedAt
	if err != nil {
		return nil, fmt.Errorf("getting server %s: %w", id, err)
	}

	hydrateServer(&s, argsJSON, envJSON, headersJSON)
	return &s, nil
}

// GetByName retrieves a server by name.
func (r *ServersRepo) GetByName(ctx context.Context, name string) (*models.Server, error) {
	var s models.Server
	var argsJSON, envJSON, headersJSON []byte
	var updatedAt time.Time

	err := r.pool.QueryRow(ctx, `
		SELECT id, name, mode, upstream_url, command, args, env, auth_header, headers, status, last_seen, created_at, updated_at
		FROM servers WHERE name = $1`, name,
	).Scan(
		&s.ID, &s.Name, &s.Mode, &s.UpstreamURL, &s.Command,
		&argsJSON, &envJSON, &s.AuthHeader, &headersJSON, &s.Status, &s.LastSeen,
		&s.CreatedAt, &updatedAt,
	)
	_ = updatedAt
	if err != nil {
		return nil, fmt.Errorf("getting server by name %q: %w", name, err)
	}

	hydrateServer(&s, argsJSON, envJSON, headersJSON)
	return &s, nil
}

// List returns all servers ordered by creation time (newest first).
func (r *ServersRepo) List(ctx context.Context) ([]models.Server, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, mode, upstream_url, command, args, env, auth_header, headers, status, last_seen, created_at, updated_at
		FROM servers ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing servers: %w", err)
	}
	defer rows.Close()

	servers := make([]models.Server, 0)
	for rows.Next() {
		var s models.Server
		var argsJSON, envJSON, headersJSON []byte
		var updatedAt time.Time

		if err := rows.Scan(
			&s.ID, &s.Name, &s.Mode, &s.UpstreamURL, &s.Command,
			&argsJSON, &envJSON, &s.AuthHeader, &headersJSON, &s.Status, &s.LastSeen,
			&s.CreatedAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning server row: %w", err)
		}

		hydrateServer(&s, argsJSON, envJSON, headersJSON)
		servers = append(servers, s)
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

	tag, err := r.pool.Exec(ctx, `
		UPDATE servers SET
			name=$1, mode=$2, upstream_url=$3, command=$4, args=$5, env=$6,
			auth_header=$7, headers=$8, status=$9, updated_at=$10
		WHERE id=$11`,
		s.Name, s.Mode, s.UpstreamURL, s.Command, argsJSON, envJSON,
		s.AuthHeader, headersJSON, s.Status, time.Now(), s.ID,
	)
	if err != nil {
		return fmt.Errorf("updating server %s: %w", s.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("server %q not found", s.ID)
	}
	return nil
}

// Delete removes a server by ID.
func (r *ServersRepo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting server %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("server %q not found", id)
	}
	return nil
}

func hydrateServer(s *models.Server, argsJSON, envJSON, headersJSON []byte) {
	if argsJSON != nil {
		_ = json.Unmarshal(argsJSON, &s.Args)
	}
	if envJSON != nil {
		_ = json.Unmarshal(envJSON, &s.Env)
	}
	if headersJSON != nil {
		_ = json.Unmarshal(headersJSON, &s.Headers)
	}
}
