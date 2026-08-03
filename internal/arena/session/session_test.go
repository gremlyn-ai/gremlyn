package session

import (
	"context"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Manager tests ──

func TestManager_CreateSession(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	cfg := Config{
		Gremlins:  []string{"hallucination", "latency"},
		Intensity: "medium",
		Prompts:   []string{"test prompt"},
	}

	sess, err := m.Create(context.Background(), "test-server", cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID())
	assert.Equal(t, models.SessionStatusRunning, sess.Status())
}

func TestManager_GetSession(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	sess, _ := m.Create(context.Background(), "server-1", Config{
		Gremlins: []string{"hallucination"},
	})

	got, ok := m.Get(sess.ID())
	assert.True(t, ok)
	assert.Equal(t, sess.ID(), got.ID())
}

func TestManager_GetSessionNotFound(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	_, ok := m.Get("nonexistent")
	assert.False(t, ok)
}

func TestManager_ListSessions(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	_, _ = m.Create(context.Background(), "s1", Config{Gremlins: []string{"a"}})
	_, _ = m.Create(context.Background(), "s2", Config{Gremlins: []string{"b"}})

	assert.Len(t, m.List(), 2)
}

func TestManager_StopSession(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	sess, _ := m.Create(context.Background(), "server", Config{
		Gremlins: []string{"hallucination"},
	})

	err := m.Stop(context.Background(), sess.ID())
	require.NoError(t, err)
	assert.Equal(t, models.SessionStatusCancelled, sess.Status())
}

func TestManager_StopSessionNotFound(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	err := m.Stop(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestManager_StopSessionNotRunning(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	sess, _ := m.Create(context.Background(), "server", Config{
		Gremlins: []string{"hallucination"},
	})
	_ = m.Stop(context.Background(), sess.ID()) // Cancel it first.

	err := m.Stop(context.Background(), sess.ID())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

// ── Session state machine tests ──

func TestSession_StatusTransitions(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	sess, _ := m.Create(context.Background(), "server", Config{
		Gremlins: []string{"hallucination"},
	})

	// Created → Running (happens at creation).
	assert.Equal(t, models.SessionStatusRunning, sess.Status())

	// Running → Cancelled.
	sess.Cancel()
	assert.Equal(t, models.SessionStatusCancelled, sess.Status())
	assert.NotNil(t, sess.Model().CompletedAt)
}

func TestSession_Complete(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	sess, _ := m.Create(context.Background(), "server", Config{
		Gremlins: []string{"hallucination"},
	})

	sess.Complete([]byte(`{"overall":75}`), 10, 7, 3)
	assert.Equal(t, models.SessionStatusCompleted, sess.Status())
	assert.NotNil(t, sess.Model().CompletedAt)
	assert.Equal(t, 10, sess.Model().GremlinsSent)
	assert.Equal(t, 7, sess.Model().GremlinsSurvived)
	assert.Equal(t, 3, sess.Model().GremlinsCrashed)
}

// ── Recorder tests ──

func TestRecorder_Record(t *testing.T) {
	rec := NewRecorder("session-1", nil, nil)

	event := models.ArenaEvent{
		ID:          "evt-1",
		GremlinType: "hallucination",
		Outcome:     models.OutcomeSurvived,
		Score:       90,
	}

	err := rec.Record(context.Background(), event)
	require.NoError(t, err)

	events := rec.Events()
	assert.Len(t, events, 1)
	assert.Equal(t, "session-1", events[0].SessionID) // Session ID set by recorder.
}

func TestRecorder_RecordWithNotifier(t *testing.T) {
	var notified models.ArenaEvent
	notifier := &mockNotifier{fn: func(e models.ArenaEvent) { notified = e }}

	rec := NewRecorder("session-1", nil, notifier)
	event := models.ArenaEvent{
		ID:          "evt-1",
		GremlinType: "corruption",
		Score:       50,
	}

	err := rec.Record(context.Background(), event)
	require.NoError(t, err)
	assert.Equal(t, "corruption", notified.GremlinType)
}

// ── Runner tests ──

func TestRunner_RunCompletesSession(t *testing.T) {
	registry := gremlins.NewRegistry()
	registry.Register(gremlins.NewHallucinationGremlin("fake_tool", 1.0))
	registry.Register(gremlins.NewLoopGremlin(3, "retry", 1.0))

	logger := zerolog.Nop()
	runner := NewRunner(registry, logger)
	manager := NewManager(nil, logger)

	sess, err := manager.Create(context.Background(), "test-server", Config{
		Gremlins: []string{"hallucination", "loop"},
		Prompts:  []string{"test prompt 1", "test prompt 2"},
	})
	require.NoError(t, err)

	rec := NewRecorder(sess.ID(), nil, nil)
	err = runner.Run(context.Background(), sess, rec)
	require.NoError(t, err)

	assert.Equal(t, models.SessionStatusCompleted, sess.Status())
	assert.NotNil(t, sess.Model().CompletedAt)
	assert.NotNil(t, sess.Model().Results)
	assert.Greater(t, len(rec.Events()), 0)
}

func TestRunner_RunWithUnknownGremlin(t *testing.T) {
	registry := gremlins.NewRegistry()
	logger := zerolog.Nop()
	runner := NewRunner(registry, logger)
	manager := NewManager(nil, logger)

	sess, _ := manager.Create(context.Background(), "test-server", Config{
		Gremlins: []string{"nonexistent"},
		Prompts:  []string{"test"},
	})

	rec := NewRecorder(sess.ID(), nil, nil)
	err := runner.Run(context.Background(), sess, rec)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown gremlin")
}

func TestRunner_RunWithDefaultPrompts(t *testing.T) {
	registry := gremlins.NewRegistry()
	registry.Register(gremlins.NewCorruptionGremlin(gremlins.CorruptionModeWrongTypes, 1.0))

	logger := zerolog.Nop()
	runner := NewRunner(registry, logger)
	manager := NewManager(nil, logger)

	sess, _ := manager.Create(context.Background(), "test-server", Config{
		Gremlins: []string{"corruption"},
		// No prompts — should use defaults.
	})

	rec := NewRecorder(sess.ID(), nil, nil)
	err := runner.Run(context.Background(), sess, rec)
	require.NoError(t, err)
	assert.Greater(t, len(rec.Events()), 0)
}

// ── Mocks ──

type mockNotifier struct {
	fn func(models.ArenaEvent)
}

func (m *mockNotifier) Notify(e models.ArenaEvent) { m.fn(e) }
