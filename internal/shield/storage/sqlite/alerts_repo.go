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

// AlertsRepo implements service.AlertStore backed by SQLite.
type AlertsRepo struct {
	db *sql.DB
}

// Insert persists a new alert and returns its generated ID.
func (r *AlertsRepo) Insert(ctx context.Context, a *models.Alert) (string, error) {
	channelsJSON, err := json.Marshal(a.NotifiedChannels)
	if err != nil {
		return "", fmt.Errorf("marshaling notified channels: %w", err)
	}

	id := uuid.New().String()
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO alerts (
			id, event_id, severity, type, message, status,
			notified_channels, created_at
		) VALUES (?,?,?,?,?,?,?,?)`,
		id, a.EventID, a.Severity, a.Type, a.Message, models.AlertStatusNew,
		string(channelsJSON), formatTime(time.Now()),
	)
	if err != nil {
		return "", fmt.Errorf("inserting alert: %w", err)
	}
	return id, nil
}

// ListRecent returns the most recent alerts.
func (r *AlertsRepo) ListRecent(ctx context.Context, limit, offset int) ([]models.Alert, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, event_id, severity, type, message, status,
			   notified_channels, created_at, resolved_at
		FROM alerts
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("listing alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	alerts := make([]models.Alert, 0)
	for rows.Next() {
		var a models.Alert
		var channelsJSON sql.NullString
		var createdAt string
		var resolvedAt sql.NullString

		if err := rows.Scan(
			&a.ID, &a.EventID, &a.Severity, &a.Type, &a.Message,
			&a.Status, &channelsJSON, &createdAt, &resolvedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning alert row: %w", err)
		}

		if channelsJSON.Valid {
			if err := json.Unmarshal([]byte(channelsJSON.String), &a.NotifiedChannels); err != nil {
				return nil, fmt.Errorf("unmarshaling notified channels: %w", err)
			}
		}

		t, err := parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parsing created_at: %w", err)
		}
		a.CreatedAt = t

		resolved, err := parseNullableTime(nullStringPtr(resolvedAt))
		if err != nil {
			return nil, fmt.Errorf("parsing resolved_at: %w", err)
		}
		a.ResolvedAt = resolved

		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

// UpdateStatus changes the status of an alert.
func (r *AlertsRepo) UpdateStatus(ctx context.Context, id string, status models.AlertStatus) error {
	var resolvedAt interface{}
	if status == models.AlertStatusResolved {
		resolvedAt = formatTime(time.Now())
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE alerts SET status = ?, resolved_at = ? WHERE id = ?`,
		status, resolvedAt, id,
	)
	if err != nil {
		return fmt.Errorf("updating alert status %s: %w", id, err)
	}
	return nil
}

// CountBySeverity returns alert counts grouped by severity for the last N hours.
func (r *AlertsRepo) CountBySeverity(ctx context.Context, hours int) (map[string]int, error) {
	cutoff := formatTime(time.Now().Add(-time.Duration(hours) * time.Hour))
	rows, err := r.db.QueryContext(ctx, `
		SELECT severity, COUNT(*)
		FROM alerts
		WHERE created_at > ?
		GROUP BY severity`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("counting alerts by severity: %w", err)
	}
	defer func() { _ = rows.Close() }()

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

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}
