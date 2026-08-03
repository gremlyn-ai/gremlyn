// Package sqlite provides SQLite-backed repository implementations for
// persisting Arena sessions and events. This is the default embedded
// storage backend — no external database server required.
package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	_ "modernc.org/sqlite" // SQLite driver (pure Go, no CGO).
)

// DB wraps a sql.DB connection to a SQLite database.
type DB struct {
	pool   *sql.DB
	logger zerolog.Logger
}

// New opens a SQLite database at the given path, applies pragmas for
// performance and correctness, and runs embedded migrations.
func New(dbPath string, logger zerolog.Logger) (*DB, error) {
	pool, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite %s: %w", dbPath, err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := pool.Exec(p); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("setting pragma %q: %w", p, err)
		}
	}

	db := &DB{pool: pool, logger: logger}
	if err := db.migrate(); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	logger.Info().Str("path", dbPath).Msg("sqlite database ready")
	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.pool.Close()
}

// Sessions returns the sessions repository.
func (db *DB) Sessions() *SessionsRepo {
	return &SessionsRepo{db: db.pool}
}

// Events returns the events repository.
func (db *DB) Events() *EventsRepo {
	return &EventsRepo{db: db.pool}
}

// migrate runs embedded SQL migrations using a simple version table.
func (db *DB) migrate() error {
	_, err := db.pool.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	var count int
	if err := db.pool.QueryRow("SELECT COUNT(*) FROM _migrations WHERE version = 1").Scan(&count); err != nil {
		return fmt.Errorf("checking migration version: %w", err)
	}
	if count > 0 {
		return nil
	}

	tx, err := db.pool.Begin()
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback on committed tx is no-op

	if _, err := tx.Exec(migrationSQL); err != nil {
		return fmt.Errorf("executing migration: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO _migrations (version, applied_at) VALUES (1, ?)", formatTime(time.Now())); err != nil {
		return fmt.Errorf("recording migration: %w", err)
	}

	return tx.Commit()
}

// ── Time helpers ──

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

func parseNullableTime(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	t, err := parseTime(*s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

// jsonOrNull returns the string representation of a json.RawMessage, or nil if empty.
func jsonOrNull(raw []byte) interface{} {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
