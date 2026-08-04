package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/shield/service"
	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newProductionShield(t *testing.T) (*service.Shield, *memRuleStore) {
	t.Helper()
	rs := &memRuleStore{}
	cfg := &config.Config{
		Version: 1,
		Mode:    "shield",
		Servers: map[string]config.ServerConfig{
			"hubspot": {
				Mode:     models.ServerModeProxy,
				Upstream: "http://localhost:3001",
			},
			"postgres": {
				Mode:     models.ServerModeProxy,
				Upstream: "http://localhost:5433/mcp",
			},
			"slack": {
				Mode:    models.ServerModeWrap,
				Command: "npx",
				Args:    []string{"-y", "@anthropic/mcp-slack"},
			},
		},
	}
	shield := service.NewShield(cfg, &memEventStore{}, rs, &memAlertStore{}, &memServerStore{}, nil, zerolog.Nop())
	return shield, rs
}

// ---------------------------------------------------------------------------
// Scenario: Create production rules via API then verify list
// ---------------------------------------------------------------------------

func TestAPIScenarios_CreateProductionRulesThenList(t *testing.T) {
	shield, _ := newProductionShield(t)
	router := NewRouter(shield, zerolog.Nop())

	// Create: block bulk export.
	body1, _ := json.Marshal(CreateRuleRequest{
		ServerID:  "hubspot",
		Name:      "block-bulk-export",
		MatchTool: "search_contacts",
		Action:    models.RuleActionBlock,
	})
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body1)))
	assert.Equal(t, http.StatusCreated, w1.Code)

	// Create: scan response injections.
	body2, _ := json.Marshal(CreateRuleRequest{
		ServerID:      "hubspot",
		Name:          "scan-response-injections",
		ScanResponses: true,
		Detect:        []string{"prompt_injection"},
		Action:        models.RuleActionBlockAndAlert,
	})
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body2)))
	assert.Equal(t, http.StatusCreated, w2.Code)

	// Create: block delete operations.
	body3, _ := json.Marshal(CreateRuleRequest{
		ServerID:  "hubspot",
		Name:      "block-delete-operations",
		MatchTool: "delete_contact",
		Action:    models.RuleActionBlockAndAlert,
	})
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body3)))
	assert.Equal(t, http.StatusCreated, w3.Code)

	// List rules for hubspot.
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, httptest.NewRequest(http.MethodGet, "/api/v1/rules?server_id=hubspot", http.NoBody))
	assert.Equal(t, http.StatusOK, w4.Code)

	var resp RuleListResponse
	require.NoError(t, json.NewDecoder(w4.Body).Decode(&resp))
	assert.Len(t, resp.Rules, 3)

	// Verify rule names.
	names := make([]string, len(resp.Rules))
	for i, r := range resp.Rules {
		names[i] = r.Name
	}
	assert.Contains(t, names, "block-bulk-export")
	assert.Contains(t, names, "scan-response-injections")
	assert.Contains(t, names, "block-delete-operations")
}

// ---------------------------------------------------------------------------
// Scenario: Get metrics after multiple blocked events
// ---------------------------------------------------------------------------

func TestAPIScenarios_MetricsAfterBlocking(t *testing.T) {
	shield, _ := newProductionShield(t)
	router := NewRouter(shield, zerolog.Nop())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?hours=24", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code)

	var resp MetricsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 24, resp.PeriodHours)
	assert.NotNil(t, resp.EventCounts)
	// memEventStore returns { "allowed": 10, "blocked": 2 }.
	assert.Equal(t, 10, resp.EventCounts["allowed"])
	assert.Equal(t, 2, resp.EventCounts["blocked"])
}

// ---------------------------------------------------------------------------
// Scenario: Server CRUD lifecycle
// ---------------------------------------------------------------------------

func TestAPIScenarios_ServerListReturnsConfigured(t *testing.T) {
	shield, _ := newProductionShield(t)
	router := NewRouter(shield, zerolog.Nop())

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/servers", http.NoBody))
	assert.Equal(t, http.StatusOK, w.Code)

	var resp ServerListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.GreaterOrEqual(t, len(resp.Servers), 3)

	// Verify server names are present.
	serverNames := make([]string, len(resp.Servers))
	for i, s := range resp.Servers {
		serverNames[i] = s.Name
	}
	assert.Contains(t, serverNames, "hubspot")
	assert.Contains(t, serverNames, "postgres")
	assert.Contains(t, serverNames, "slack")
}

// ---------------------------------------------------------------------------
// Scenario: Rule creation validation — missing name
// ---------------------------------------------------------------------------

func TestAPIScenarios_RuleValidationRejectsEmptyName(t *testing.T) {
	shield, _ := newProductionShield(t)
	router := NewRouter(shield, zerolog.Nop())

	body, _ := json.Marshal(CreateRuleRequest{
		ServerID: "hubspot",
		Name:     "",
		Action:   models.RuleActionBlock,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/rules", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Scenario: Status shows running after start
// ---------------------------------------------------------------------------

func TestAPIScenarios_StatusLifecycle(t *testing.T) {
	shield, _ := newProductionShield(t)
	router := NewRouter(shield, zerolog.Nop())

	// Initially not running.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/status", http.NoBody))
	var status StatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&status))
	assert.False(t, status.Running)

	// Start.
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/api/v1/start", http.NoBody))
	assert.Equal(t, http.StatusOK, w2.Code)

	// Now running.
	w3 := httptest.NewRecorder()
	router.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/api/v1/status", http.NoBody))
	var status2 StatusResponse
	require.NoError(t, json.NewDecoder(w3.Body).Decode(&status2))
	assert.True(t, status2.Running)
	assert.GreaterOrEqual(t, len(status2.Servers), 3)

	// Stop.
	w4 := httptest.NewRecorder()
	router.ServeHTTP(w4, httptest.NewRequest(http.MethodPost, "/api/v1/stop", http.NoBody))
	assert.Equal(t, http.StatusOK, w4.Code)
}
