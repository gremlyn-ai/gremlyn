package service

import (
	"context"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memEventStore is an in-memory EventStore for testing.
type memEventStore struct {
	events []models.Event
}

func (m *memEventStore) Insert(_ context.Context, e *models.Event) (string, error) {
	e.ID = "evt-1"
	m.events = append(m.events, *e)
	return e.ID, nil
}

func (m *memEventStore) ListByServer(_ context.Context, _ string, limit, _ int) ([]models.Event, error) {
	if limit > len(m.events) {
		limit = len(m.events)
	}
	return m.events[:limit], nil
}

func (m *memEventStore) ListBlocked(_ context.Context, _, _ int) ([]models.Event, error) {
	var blocked []models.Event
	for _, e := range m.events {
		if e.ActionTaken == models.ActionBlocked {
			blocked = append(blocked, e)
		}
	}
	return blocked, nil
}

func (m *memEventStore) CountByAction(_ context.Context, _ int) (map[string]int, error) {
	counts := make(map[string]int)
	for _, e := range m.events {
		counts[string(e.ActionTaken)]++
	}
	return counts, nil
}

// memRuleStore is an in-memory RuleStore for testing.
type memRuleStore struct {
	rules []models.Rule
}

func (m *memRuleStore) Insert(_ context.Context, r *models.Rule) (string, error) {
	r.ID = "rule-1"
	m.rules = append(m.rules, *r)
	return r.ID, nil
}

func (m *memRuleStore) GetByID(_ context.Context, id string) (*models.Rule, error) {
	for _, r := range m.rules {
		if r.ID == id {
			return &r, nil
		}
	}
	return nil, nil
}

func (m *memRuleStore) ListByServer(_ context.Context, _ string) ([]models.Rule, error) {
	return m.rules, nil
}

func (m *memRuleStore) ListEnabled(_ context.Context) ([]models.Rule, error) {
	var enabled []models.Rule
	for _, r := range m.rules {
		if r.Enabled {
			enabled = append(enabled, r)
		}
	}
	return enabled, nil
}

func (m *memRuleStore) Update(_ context.Context, _ *models.Rule) error { return nil }
func (m *memRuleStore) Delete(_ context.Context, _ string) error       { return nil }

// memAlertStore is an in-memory AlertStore for testing.
type memAlertStore struct {
	alerts []models.Alert
}

func (m *memAlertStore) Insert(_ context.Context, a *models.Alert) (string, error) {
	a.ID = "alert-1"
	m.alerts = append(m.alerts, *a)
	return a.ID, nil
}

func (m *memAlertStore) ListRecent(_ context.Context, _, _ int) ([]models.Alert, error) {
	return m.alerts, nil
}

func (m *memAlertStore) UpdateStatus(_ context.Context, _ string, _ models.AlertStatus) error {
	return nil
}

func (m *memAlertStore) CountBySeverity(_ context.Context, _ int) (map[string]int, error) {
	return map[string]int{}, nil
}

// memServerStore is an in-memory ServerStore for testing.
type memServerStore struct {
	servers []models.Server
}

func (m *memServerStore) Insert(_ context.Context, s *models.Server) (string, error) {
	s.ID = "srv-new"
	m.servers = append(m.servers, *s)
	return s.ID, nil
}
func (m *memServerStore) GetByID(_ context.Context, _ string) (*models.Server, error) {
	return nil, nil
}
func (m *memServerStore) GetByName(_ context.Context, _ string) (*models.Server, error) {
	return nil, nil
}
func (m *memServerStore) List(_ context.Context) ([]models.Server, error) { return m.servers, nil }
func (m *memServerStore) Update(_ context.Context, _ *models.Server) error { return nil }
func (m *memServerStore) Delete(_ context.Context, _ string) error         { return nil }

func TestShield_StartStop(t *testing.T) {
	cfg := &config.Config{
		Version: 1,
		Mode:    "shield",
		Servers: map[string]config.ServerConfig{
			"hubspot": {
				Mode: models.ServerModeWrap,
				Rules: []config.RuleConfig{
					{Name: "test rule", Match: &config.MatchConfig{Tool: "search"}, Action: models.RuleActionBlock},
				},
			},
		},
	}

	logger := zerolog.Nop()
	shield := NewShield(cfg, &memEventStore{}, &memRuleStore{}, &memAlertStore{}, &memServerStore{}, nil, logger)

	assert.False(t, shield.IsRunning())

	err := shield.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, shield.IsRunning())
	assert.NotNil(t, shield.Pipeline())

	// Double start should error.
	err = shield.Start(context.Background())
	assert.Error(t, err)

	shield.Stop()
	assert.False(t, shield.IsRunning())
}

func TestShield_StoreAccessors(t *testing.T) {
	events := &memEventStore{}
	rules := &memRuleStore{}
	alerts := &memAlertStore{}

	shield := NewShield(&config.Config{Servers: map[string]config.ServerConfig{}}, events, rules, alerts, &memServerStore{}, nil, zerolog.Nop())
	assert.Equal(t, events, shield.Events())
	assert.Equal(t, rules, shield.Rules())
	assert.Equal(t, alerts, shield.Alerts())
}
