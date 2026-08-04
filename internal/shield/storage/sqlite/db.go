// Package sqlite provides SQLite-backed repository implementations for
// persisting Shield events, rules, and alerts. This is the default
// embedded storage backend — no external database server required.
package sqlite

import (
	"context"
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
func New(ctx context.Context, dbPath string, logger zerolog.Logger) (*DB, error) {
	pool, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite %s: %w", dbPath, err)
	}

	// Performance and correctness pragmas.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	}
	for _, p := range pragmas {
		if _, err := pool.ExecContext(ctx, p); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("setting pragma %q: %w", p, err)
		}
	}

	db := &DB{pool: pool, logger: logger}
	if err := db.migrate(ctx); err != nil {
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

// Events returns the events repository.
func (db *DB) Events() *EventsRepo {
	return &EventsRepo{db: db.pool}
}

// Rules returns the rules repository.
func (db *DB) Rules() *RulesRepo {
	return &RulesRepo{db: db.pool}
}

// Alerts returns the alerts repository.
func (db *DB) Alerts() *AlertsRepo {
	return &AlertsRepo{db: db.pool}
}

// Servers returns the servers repository.
func (db *DB) Servers() *ServersRepo {
	return &ServersRepo{db: db.pool}
}

// migrate runs embedded SQL migrations using a simple version table.
func (db *DB) migrate(ctx context.Context) error {
	_, err := db.pool.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{1, migrationSQL},
		{2, migrationServersSQL},
	}

	for _, m := range migrations {
		var count int
		if err := db.pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM _migrations WHERE version = ?", m.version).Scan(&count); err != nil {
			return fmt.Errorf("checking migration version %d: %w", m.version, err)
		}
		if count > 0 {
			continue
		}

		if err := db.applyMigration(ctx, m.version, m.sql); err != nil {
			return err
		}
		db.logger.Info().Int("version", m.version).Msg("migration applied")
	}

	return nil
}

// applyMigration runs a single migration and records its version in one
// transaction. It lives in its own function so the deferred rollback releases
// the transaction per migration rather than accumulating until migrate returns.
func (db *DB) applyMigration(ctx context.Context, version int, migration string) error {
	tx, err := db.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration v%d tx: %w", version, err)
	}
	// Rollback on an already-committed transaction is a no-op.
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("executing migration v%d: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO _migrations (version, applied_at) VALUES (?, ?)", version, formatTime(time.Now())); err != nil {
		return fmt.Errorf("recording migration v%d: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing migration v%d: %w", version, err)
	}
	return nil
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
