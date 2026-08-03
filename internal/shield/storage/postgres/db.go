// Package postgres provides PostgreSQL repository implementations for
// persisting Shield events, rules, and alerts using pgx.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// DB wraps a pgx connection pool and provides access to domain repositories.
type DB struct {
	Pool   *pgxpool.Pool
	Logger zerolog.Logger
}

// New creates a new DB from a connection string and returns it.
func New(ctx context.Context, connString string, logger zerolog.Logger) (*DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	logger.Info().Msg("connected to postgres")
	return &DB{Pool: pool, Logger: logger}, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// Events returns the events repository.
func (db *DB) Events() *EventsRepo {
	return &EventsRepo{pool: db.Pool, logger: db.Logger}
}

// Rules returns the rules repository.
func (db *DB) Rules() *RulesRepo {
	return &RulesRepo{pool: db.Pool, logger: db.Logger}
}

// Alerts returns the alerts repository.
func (db *DB) Alerts() *AlertsRepo {
	return &AlertsRepo{pool: db.Pool, logger: db.Logger}
}

// Servers returns the servers repository.
func (db *DB) Servers() *ServersRepo {
	return &ServersRepo{pool: db.Pool, logger: db.Logger}
}
