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

// RulesRepo implements service.RuleStore backed by SQLite.
type RulesRepo struct {
	db *sql.DB
}

// Insert creates a new rule and returns its generated ID.
func (r *RulesRepo) Insert(ctx context.Context, rule *models.Rule) (string, error) {
	matchJSON, err := json.Marshal(rule.Match)
	if err != nil {
		return "", fmt.Errorf("marshaling rule match: %w", err)
	}
	detectJSON, err := json.Marshal(rule.Detect)
	if err != nil {
		return "", fmt.Errorf("marshaling rule detect: %w", err)
	}

	id := uuid.New().String()
	now := formatTime(time.Now())
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO rules (
			id, server_id, name, match_config, scan_responses, scan_outgoing,
			detect, entity_field, action, enabled, source,
			original_text, config, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, rule.ServerID, rule.Name, string(matchJSON), boolToInt(rule.ScanResponses),
		boolToInt(rule.ScanOutgoing), string(detectJSON), rule.EntityField, rule.Action,
		boolToInt(true), rule.Source, rule.OriginalText,
		jsonOrNull(rule.Config), now, now,
	)
	if err != nil {
		return "", fmt.Errorf("inserting rule: %w", err)
	}
	return id, nil
}

// GetByID retrieves a single rule by ID.
func (r *RulesRepo) GetByID(ctx context.Context, id string) (*models.Rule, error) {
	var rule models.Rule
	var matchJSON, detectJSON sql.NullString
	var configStr sql.NullString
	var scanResp, scanOut, enabled int
	var createdAt, updatedAt string

	err := r.db.QueryRowContext(ctx, `
		SELECT id, server_id, name, match_config, scan_responses, scan_outgoing,
			   detect, entity_field, action, enabled, source,
			   original_text, config, created_at, updated_at
		FROM rules WHERE id = ?`, id,
	).Scan(
		&rule.ID, &rule.ServerID, &rule.Name, &matchJSON, &scanResp,
		&scanOut, &detectJSON, &rule.EntityField, &rule.Action,
		&enabled, &rule.Source, &rule.OriginalText, &configStr,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("getting rule %s: %w", id, err)
	}

	rule.ScanResponses = scanResp != 0
	rule.ScanOutgoing = scanOut != 0
	rule.Enabled = enabled != 0

	if matchJSON.Valid {
		if err := json.Unmarshal([]byte(matchJSON.String), &rule.Match); err != nil {
			return nil, fmt.Errorf("unmarshaling rule match: %w", err)
		}
	}
	if detectJSON.Valid {
		if err := json.Unmarshal([]byte(detectJSON.String), &rule.Detect); err != nil {
			return nil, fmt.Errorf("unmarshaling rule detect: %w", err)
		}
	}
	if configStr.Valid {
		rule.Config = json.RawMessage(configStr.String)
	}

	t, err := parseTime(createdAt)
	if err != nil {
		return nil, fmt.Errorf("parsing created_at: %w", err)
	}
	rule.CreatedAt = t

	t, err = parseTime(updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parsing updated_at: %w", err)
	}
	rule.UpdatedAt = t

	return &rule, nil
}

// ListByServer returns all rules for a given server.
func (r *RulesRepo) ListByServer(ctx context.Context, serverID string) ([]models.Rule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_id, name, match_config, scan_responses, scan_outgoing,
			   detect, entity_field, action, enabled, source,
			   original_text, config, created_at, updated_at
		FROM rules WHERE server_id = ?
		ORDER BY created_at ASC`, serverID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing rules for server %s: %w", serverID, err)
	}
	defer func() { _ = rows.Close() }()

	return scanRules(rows)
}

// ListEnabled returns all enabled rules across all servers.
func (r *RulesRepo) ListEnabled(ctx context.Context) ([]models.Rule, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, server_id, name, match_config, scan_responses, scan_outgoing,
			   detect, entity_field, action, enabled, source,
			   original_text, config, created_at, updated_at
		FROM rules WHERE enabled = 1
		ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing enabled rules: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanRules(rows)
}

// Update modifies an existing rule.
func (r *RulesRepo) Update(ctx context.Context, rule *models.Rule) error {
	matchJSON, err := json.Marshal(rule.Match)
	if err != nil {
		return fmt.Errorf("marshaling rule match: %w", err)
	}
	detectJSON, err := json.Marshal(rule.Detect)
	if err != nil {
		return fmt.Errorf("marshaling rule detect: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		UPDATE rules SET
			name=?, match_config=?, scan_responses=?, scan_outgoing=?,
			detect=?, entity_field=?, action=?, enabled=?,
			original_text=?, config=?, updated_at=?
		WHERE id=?`,
		rule.Name, string(matchJSON), boolToInt(rule.ScanResponses), boolToInt(rule.ScanOutgoing),
		string(detectJSON), rule.EntityField, rule.Action, boolToInt(rule.Enabled),
		rule.OriginalText, jsonOrNull(rule.Config), formatTime(time.Now()), rule.ID,
	)
	if err != nil {
		return fmt.Errorf("updating rule %s: %w", rule.ID, err)
	}
	return nil
}

// Delete removes a rule by ID.
func (r *RulesRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM rules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting rule %s: %w", id, err)
	}
	return nil
}

func scanRules(rows *sql.Rows) ([]models.Rule, error) {
	rules := make([]models.Rule, 0)
	for rows.Next() {
		var rule models.Rule
		var matchJSON, detectJSON, configStr sql.NullString
		var scanResp, scanOut, enabled int
		var createdAt, updatedAt string

		if err := rows.Scan(
			&rule.ID, &rule.ServerID, &rule.Name, &matchJSON, &scanResp,
			&scanOut, &detectJSON, &rule.EntityField, &rule.Action,
			&enabled, &rule.Source, &rule.OriginalText, &configStr,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning rule row: %w", err)
		}

		rule.ScanResponses = scanResp != 0
		rule.ScanOutgoing = scanOut != 0
		rule.Enabled = enabled != 0

		if matchJSON.Valid {
			if err := json.Unmarshal([]byte(matchJSON.String), &rule.Match); err != nil {
				return nil, fmt.Errorf("unmarshaling rule match: %w", err)
			}
		}
		if detectJSON.Valid {
			if err := json.Unmarshal([]byte(detectJSON.String), &rule.Detect); err != nil {
				return nil, fmt.Errorf("unmarshaling rule detect: %w", err)
			}
		}
		if configStr.Valid {
			rule.Config = json.RawMessage(configStr.String)
		}

		t, err := parseTime(createdAt)
		if err != nil {
			return nil, fmt.Errorf("parsing created_at: %w", err)
		}
		rule.CreatedAt = t

		t, err = parseTime(updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parsing updated_at: %w", err)
		}
		rule.UpdatedAt = t

		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
