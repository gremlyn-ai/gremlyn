package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// AlertsRepo provides CRUD operations for shield security alerts.
type AlertsRepo struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

// Insert persists a new alert and returns its generated ID.
func (r *AlertsRepo) Insert(ctx context.Context, a *models.Alert) (string, error) {
	var id string
	err := r.pool.QueryRow(ctx, `
		INSERT INTO alerts (
			event_id, severity, type, message, status,
			notified_channels, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id`,
		a.EventID, a.Severity, a.Type, a.Message, models.AlertStatusNew,
		a.NotifiedChannels, time.Now(),
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("inserting alert: %w", err)
	}
	return id, nil
}

// ListRecent returns the most recent alerts.
func (r *AlertsRepo) ListRecent(ctx context.Context, limit, offset int) ([]models.Alert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, event_id, severity, type, message, status,
			   notified_channels, created_at, resolved_at
		FROM alerts
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("listing alerts: %w", err)
	}
	defer rows.Close()

	alerts := make([]models.Alert, 0)
	for rows.Next() {
		var a models.Alert
		if err := rows.Scan(
			&a.ID, &a.EventID, &a.Severity, &a.Type, &a.Message,
			&a.Status, &a.NotifiedChannels, &a.CreatedAt, &a.ResolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning alert row: %w", err)
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// UpdateStatus changes the status of an alert.
func (r *AlertsRepo) UpdateStatus(ctx context.Context, id string, status models.AlertStatus) error {
	var resolvedAt *time.Time
	if status == models.AlertStatusResolved {
		now := time.Now()
		resolvedAt = &now
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE alerts SET status = $1, resolved_at = $2 WHERE id = $3`,
		status, resolvedAt, id,
	)
	if err != nil {
		return fmt.Errorf("updating alert status %s: %w", id, err)
	}
	return nil
}

// CountBySeverity returns alert counts grouped by severity for the last N hours.
func (r *AlertsRepo) CountBySeverity(ctx context.Context, hours int) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT severity, COUNT(*)
		FROM alerts
		WHERE created_at > NOW() - INTERVAL '1 hour' * $1
		GROUP BY severity`,
		hours,
	)
	if err != nil {
		return nil, fmt.Errorf("counting alerts by severity: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var sev string
		var count int
		if err := rows.Scan(&sev, &count); err != nil {
			return nil, fmt.Errorf("scanning alert count: %w", err)
		}
		counts[sev] = count
	}
	return counts, rows.Err()
}
