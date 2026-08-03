package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/internal/shield/service"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memEventStore is an in-memory EventStore for API tests.
type memEventStore struct {
	events []models.Event
}

func (m *memEventStore) Insert(_ context.Context, e *models.Event) (string, error) {
	e.ID = "evt-1"
	m.events = append(m.events, *e)
	return e.ID, nil
}

func (m *memEventStore) ListByServer(_ context.Context, _ string, _, _ int) ([]models.Event, error) {
	return m.events, nil
}

func (m *memEventStore) ListBlocked(_ context.Context, _, _ int) ([]models.Event, error) {
	return nil, nil
}

func (m *memEventStore) CountByAction(_ context.Context, _ int) (map[string]int, error) {
	return map[string]int{"allowed": 10, "blocked": 2}, nil
}

// memRuleStore is an in-memory RuleStore for API tests.
type memRuleStore struct {
	rules []models.Rule
}

func (m *memRuleStore) Insert(_ context.Context, r *models.Rule) (string, error) {
	r.ID = "rule-new"
	m.rules = append(m.rules, *r)
	return r.ID, nil
}

func (m *memRuleStore) GetByID(_ context.Context, _ string) (*models.Rule, error) { return nil, nil }
func (m *memRuleStore) ListByServer(_ context.Context, _ string) ([]models.Rule, error) {
	return m.rules, nil
}

func (m *memRuleStore) ListEnabled(_ context.Context) ([]models.Rule, error) { return m.rules, nil }
func (m *memRuleStore) Update(_ context.Context, _ *models.Rule) error       { return nil }
func (m *memRuleStore) Delete(_ context.Context, _ string) error             { return nil }

// memAlertStore is an in-memory AlertStore for API tests.
type memAlertStore struct{}

func (m *memAlertStore) Insert(_ context.Context, a *models.Alert) (string, error) {
	return "alert-1", nil
}

func (m *memAlertStore) ListRecent(_ context.Context, _, _ int) ([]models.Alert, error) {
	return []models.Alert{{ID: "a1", Severity: models.AlertSeverityMedium, Message: "test"}}, nil
}

func (m *memAlertStore) UpdateStatus(_ context.Context, _ string, _ models.AlertStatus) error {
	return nil
}

func (m *memAlertStore) CountBySeverity(_ context.Context, _ int) (map[string]int, error) {
	return map[string]int{"high": 1}, nil
}

// memServerStore is an in-memory ServerStore for API tests.
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

func newTestShield(t *testing.T) *service.Shield {
	t.Helper()
	cfg := &config.Config{
		Version: 1,
		Mode:    "shield",
		Servers: map[string]config.ServerConfig{
			"test": {Mode: models.ServerModeWrap},
		},
	}
	return service.NewShield(cfg, &memEventStore{}, &memRuleStore{}, &memAlertStore{}, &memServerStore{}, nil, zerolog.Nop())
}

func TestGetStatus(t *testing.T) {
	shield := newTestShield(t)
	router := NewRouter(shield, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp StatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.False(t, resp.Running)
}

func TestPostStartStop(t *testing.T) {
	shield := newTestShield(t)
	router := NewRouter(shield, zerolog.Nop())

	// Start.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/start", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Double start → conflict.
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/v1/start", http.NoBody))
	assert.Equal(t, http.StatusConflict, w2.Code)

	// Stop.
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/api/v1/stop", http.NoBody))
	assert.Equal(t, http.StatusOK, w3.Code)
}

func TestListEvents(t *testing.T) {
	shield := newTestShield(t)
	router := NewRouter(shield, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?server_id=test", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp EventListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Empty(t, resp.Events)
}

func TestCreateRule(t *testing.T) {
	shield := newTestShield(t)
	router := NewRouter(shield, zerolog.Nop())

	body, _ := json.Marshal(CreateRuleRequest{
		ServerID: "test",
		Name:     "Block search",
		Action:   models.RuleActionBlock,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var rule models.Rule
	require.NoError(t, json.NewDecoder(w.Body).Decode(&rule))
	assert.Equal(t, "rule-new", rule.ID)
	assert.Equal(t, "Block search", rule.Name)
}

func TestCreateRuleValidation(t *testing.T) {
	shield := newTestShield(t)
	router := NewRouter(shield, zerolog.Nop())

	// Missing name.
	body, _ := json.Marshal(CreateRuleRequest{Action: models.RuleActionBlock})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListAlerts(t *testing.T) {
	shield := newTestShield(t)
	router := NewRouter(shield, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp AlertListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Len(t, resp.Alerts, 1)
}

func TestGetMetrics(t *testing.T) {
	shield := newTestShield(t)
	router := NewRouter(shield, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics?hours=12", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp MetricsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 12, resp.PeriodHours)
	assert.Equal(t, 10, resp.EventCounts["allowed"])
}

func TestParsePagination(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test?limit=20&offset=5", http.NoBody)
	limit, offset := parsePagination(req)
	assert.Equal(t, 20, limit)
	assert.Equal(t, 5, offset)

	// Defaults.
	req2 := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	limit2, offset2 := parsePagination(req2)
	assert.Equal(t, 50, limit2)
	assert.Equal(t, 0, offset2)
}
