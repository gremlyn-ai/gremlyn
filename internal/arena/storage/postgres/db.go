// Package postgres provides PostgreSQL-backed repositories for Gremlyn Arena.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// DB wraps a pgx connection pool and provides access to domain repositories.
type DB struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
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
	return &DB{pool: pool, logger: logger}, nil
}

// Close closes the connection pool.
func (db *DB) Close() {
	db.Pool().Close()
}

// Pool returns the underlying connection pool.
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Sessions returns the sessions repository.
func (db *DB) Sessions() *SessionsRepo {
	return NewSessionsRepo(db.pool)
}

// Events returns the events repository.
func (db *DB) Events() *EventsRepo {
	return NewEventsRepo(db.pool)
}
