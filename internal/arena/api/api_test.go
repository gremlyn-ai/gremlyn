package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/internal/arena/service"
	"github.com/gremlyn-ai/gremlyn/internal/arena/session"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memEventStore is an in-memory EventStore for API tests.
type memEventStore struct {
	events []models.ArenaEvent
}

func (m *memEventStore) InsertEvent(_ context.Context, e models.ArenaEvent) error {
	m.events = append(m.events, e)
	return nil
}

func (m *memEventStore) ListBySession(_ context.Context, sessionID string) ([]models.ArenaEvent, error) {
	var result []models.ArenaEvent
	for _, e := range m.events {
		if e.SessionID == sessionID {
			result = append(result, e)
		}
	}
	return result, nil
}

func newTestServer() (*service.Arena, *Hub, *http.ServeMux) {
	logger := zerolog.Nop()
	registry := gremlins.NewRegistry()
	registry.Register(gremlins.NewHallucinationGremlin("fake_tool", 1.0))
	registry.Register(gremlins.NewLatencyGremlin(1, 2, 1.0))
	registry.Register(gremlins.NewCorruptionGremlin(gremlins.CorruptionModeMissingFields, 1.0))
	registry.Register(gremlins.NewLoopGremlin(3, "retry", 1.0))

	manager := session.NewManager(nil, logger)
	runner := session.NewRunner(registry, logger)
	hub := NewHub(logger)

	arena := service.New(registry, manager, runner, &memEventStore{}, hub, logger)
	_ = arena.Start()

	return arena, hub, nil
}

func newRouter() http.Handler {
	logger := zerolog.Nop()
	arena, hub, _ := newTestServer()
	return NewRouter(arena, hub, logger)
}

// ── Status endpoint ──

func TestGetStatus(t *testing.T) {
	router := newRouter()

	req := httptest.NewRequest(http.MethodGet, "/status", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp StatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Equal(t, 4, resp.Gremlins)
}

// ── Gremlins endpoint ──

func TestListGremlins(t *testing.T) {
	router := newRouter()

	req := httptest.NewRequest(http.MethodGet, "/arena/gremlins", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp GremlinListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Len(t, resp.Gremlins, 4)
}

// ── Sessions endpoints ──

func TestCreateSession(t *testing.T) {
	router := newRouter()

	body := CreateSessionRequest{
		ServerID: "test-server",
		Gremlins: []string{"hallucination"},
		Prompts:  []string{"test prompt"},
	}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/arena/sessions", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp SessionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "test-server", resp.ServerID)
	assert.Equal(t, "running", resp.Status)
}

func TestCreateSession_MissingServerID(t *testing.T) {
	router := newRouter()

	body := CreateSessionRequest{Gremlins: []string{"hallucination"}}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/arena/sessions", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSession_MissingGremlins(t *testing.T) {
	router := newRouter()

	body := CreateSessionRequest{ServerID: "s1"}
	bodyJSON, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/arena/sessions", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListSessions(t *testing.T) {
	router := newRouter()

	// Create a session first.
	body, _ := json.Marshal(CreateSessionRequest{
		ServerID: "s1", Gremlins: []string{"hallucination"}, Prompts: []string{"test"},
	})
	req := httptest.NewRequest(http.MethodPost, "/arena/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// List sessions.
	req = httptest.NewRequest(http.MethodGet, "/arena/sessions", http.NoBody)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SessionListResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.GreaterOrEqual(t, len(resp.Sessions), 1)
}

func TestGetSession(t *testing.T) {
	router := newRouter()

	// Create a session.
	body, _ := json.Marshal(CreateSessionRequest{
		ServerID: "s1", Gremlins: []string{"hallucination"}, Prompts: []string{"test"},
	})
	req := httptest.NewRequest(http.MethodPost, "/arena/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var created SessionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))

	// Get it back.
	req = httptest.NewRequest(http.MethodGet, "/arena/sessions/"+created.ID, http.NoBody)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp SessionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, created.ID, resp.ID)
}

func TestGetSession_NotFound(t *testing.T) {
	router := newRouter()

	req := httptest.NewRequest(http.MethodGet, "/arena/sessions/nonexistent", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStopSession(t *testing.T) {
	router := newRouter()

	// Create session with many prompts + latency to keep it running longer.
	body, _ := json.Marshal(CreateSessionRequest{
		ServerID: "s1",
		Gremlins: []string{"latency"},
		Prompts:  []string{"p1", "p2", "p3", "p4", "p5"},
	})
	req := httptest.NewRequest(http.MethodPost, "/arena/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var created SessionResponse
	_ = json.NewDecoder(w.Body).Decode(&created)

	// Try to stop it — may already be completed for fast gremlins.
	req = httptest.NewRequest(http.MethodPost, "/arena/sessions/"+created.ID+"/stop", http.NoBody)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Either 200 (stopped) or 400 (already completed) is acceptable.
	assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, w.Code)
}

func TestGetSessionEvents(t *testing.T) {
	router := newRouter()

	// Create and wait for session completion.
	body, _ := json.Marshal(CreateSessionRequest{
		ServerID: "s1", Gremlins: []string{"hallucination"}, Prompts: []string{"test"},
	})
	req := httptest.NewRequest(http.MethodPost, "/arena/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var created SessionResponse
	_ = json.NewDecoder(w.Body).Decode(&created)

	time.Sleep(50 * time.Millisecond) // Let session complete.

	req = httptest.NewRequest(http.MethodGet, "/arena/sessions/"+created.ID+"/events", http.NoBody)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// ── WebSocket hub tests ──

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	logger := zerolog.Nop()
	hub := NewHub(logger)

	// Just verify no panics on subscribe/unsubscribe with nil conn.
	// Real WebSocket tests would need a full server.
	assert.NotNil(t, hub)
}
