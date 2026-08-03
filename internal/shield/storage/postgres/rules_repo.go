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

// RulesRepo provides CRUD operations for shield policy rules.
type RulesRepo struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
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

	var id string
	now := time.Now()
	err = r.pool.QueryRow(ctx, `
		INSERT INTO rules (
			server_id, name, match_config, scan_responses, scan_outgoing,
			detect, entity_field, action, enabled, source,
			original_text, config, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id`,
		rule.ServerID, rule.Name, matchJSON, rule.ScanResponses, rule.ScanOutgoing,
		detectJSON, rule.EntityField, rule.Action, true, rule.Source,
		rule.OriginalText, rule.Config, now, now,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("inserting rule: %w", err)
	}
	return id, nil
}

// GetByID retrieves a single rule by ID.
func (r *RulesRepo) GetByID(ctx context.Context, id string) (*models.Rule, error) {
	var rule models.Rule
	var matchJSON, detectJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, server_id, name, match_config, scan_responses, scan_outgoing,
			   detect, entity_field, action, enabled, source,
			   original_text, config, created_at, updated_at
		FROM rules WHERE id = $1`, id,
	).Scan(
		&rule.ID, &rule.ServerID, &rule.Name, &matchJSON, &rule.ScanResponses,
		&rule.ScanOutgoing, &detectJSON, &rule.EntityField, &rule.Action,
		&rule.Enabled, &rule.Source, &rule.OriginalText, &rule.Config,
		&rule.CreatedAt, &rule.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("getting rule %s: %w", id, err)
	}

	if matchJSON != nil {
		if err := json.Unmarshal(matchJSON, &rule.Match); err != nil {
			return nil, fmt.Errorf("unmarshaling rule match: %w", err)
		}
	}
	if detectJSON != nil {
		if err := json.Unmarshal(detectJSON, &rule.Detect); err != nil {
			return nil, fmt.Errorf("unmarshaling rule detect: %w", err)
		}
	}
	return &rule, nil
}

// ListByServer returns all rules for a given server.
func (r *RulesRepo) ListByServer(ctx context.Context, serverID string) ([]models.Rule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, match_config, scan_responses, scan_outgoing,
			   detect, entity_field, action, enabled, source,
			   original_text, config, created_at, updated_at
		FROM rules WHERE server_id = $1
		ORDER BY created_at ASC`, serverID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing rules for server %s: %w", serverID, err)
	}
	defer rows.Close()

	return scanRules(rows)
}

// ListEnabled returns all enabled rules across all servers.
func (r *RulesRepo) ListEnabled(ctx context.Context) ([]models.Rule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, server_id, name, match_config, scan_responses, scan_outgoing,
			   detect, entity_field, action, enabled, source,
			   original_text, config, created_at, updated_at
		FROM rules WHERE enabled = true
		ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("listing enabled rules: %w", err)
	}
	defer rows.Close()

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

	_, err = r.pool.Exec(ctx, `
		UPDATE rules SET
			name=$1, match_config=$2, scan_responses=$3, scan_outgoing=$4,
			detect=$5, entity_field=$6, action=$7, enabled=$8,
			original_text=$9, config=$10, updated_at=$11
		WHERE id=$12`,
		rule.Name, matchJSON, rule.ScanResponses, rule.ScanOutgoing,
		detectJSON, rule.EntityField, rule.Action, rule.Enabled,
		rule.OriginalText, rule.Config, time.Now(), rule.ID,
	)
	if err != nil {
		return fmt.Errorf("updating rule %s: %w", rule.ID, err)
	}
	return nil
}

// Delete removes a rule by ID.
func (r *RulesRepo) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM rules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting rule %s: %w", id, err)
	}
	return nil
}

func scanRules(rows rowScanner) ([]models.Rule, error) {
	rules := make([]models.Rule, 0)
	for rows.Next() {
		var rule models.Rule
		var matchJSON, detectJSON []byte

		if err := rows.Scan(
			&rule.ID, &rule.ServerID, &rule.Name, &matchJSON, &rule.ScanResponses,
			&rule.ScanOutgoing, &detectJSON, &rule.EntityField, &rule.Action,
			&rule.Enabled, &rule.Source, &rule.OriginalText, &rule.Config,
			&rule.CreatedAt, &rule.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning rule row: %w", err)
		}

		if matchJSON != nil {
			if err := json.Unmarshal(matchJSON, &rule.Match); err != nil {
				return nil, fmt.Errorf("unmarshaling rule match: %w", err)
			}
		}
		if detectJSON != nil {
			if err := json.Unmarshal(detectJSON, &rule.Detect); err != nil {
				return nil, fmt.Errorf("unmarshaling rule detect: %w", err)
			}
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}
