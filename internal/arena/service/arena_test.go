package service

import (
	"context"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/internal/arena/session"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestArena() *Arena {
	logger := zerolog.Nop()
	registry := gremlins.NewRegistry()
	registry.Register(gremlins.NewHallucinationGremlin("fake_tool", 1.0))
	registry.Register(gremlins.NewLatencyGremlin(1, 2, 1.0))
	registry.Register(gremlins.NewCorruptionGremlin(gremlins.CorruptionModeMissingFields, 1.0))
	registry.Register(gremlins.NewLoopGremlin(3, "retry", 1.0))

	manager := session.NewManager(nil, logger)
	runner := session.NewRunner(registry, logger)

	return New(registry, manager, runner, nil, nil, logger)
}

func TestArena_StartStop(t *testing.T) {
	a := newTestArena()
	require.NoError(t, a.Start())
	a.Stop()
}

func TestArena_DoubleStart(t *testing.T) {
	a := newTestArena()
	require.NoError(t, a.Start())
	defer a.Stop()

	err := a.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already started")
}

func TestArena_StartSession(t *testing.T) {
	a := newTestArena()
	require.NoError(t, a.Start())
	defer a.Stop()

	sess, err := a.StartSession(context.Background(), "test-server", session.Config{
		Gremlins: []string{"hallucination"},
		Prompts:  []string{"test"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID())

	// Wait for the async run to complete.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, models.SessionStatusCompleted, sess.Status())
}

func TestArena_ListGremlins(t *testing.T) {
	a := newTestArena()
	gs := a.ListGremlins()
	assert.Len(t, gs, 4)
}

func TestArena_ListSessions(t *testing.T) {
	a := newTestArena()
	require.NoError(t, a.Start())
	defer a.Stop()

	_, _ = a.StartSession(context.Background(), "s1", session.Config{
		Gremlins: []string{"hallucination"},
		Prompts:  []string{"test"},
	})
	_, _ = a.StartSession(context.Background(), "s2", session.Config{
		Gremlins: []string{"latency"},
		Prompts:  []string{"test"},
	})

	assert.Len(t, a.ListSessions(), 2)
}

func TestArena_GetSession(t *testing.T) {
	a := newTestArena()
	require.NoError(t, a.Start())
	defer a.Stop()

	sess, _ := a.StartSession(context.Background(), "s1", session.Config{
		Gremlins: []string{"hallucination"},
		Prompts:  []string{"test"},
	})

	got, ok := a.GetSession(sess.ID())
	assert.True(t, ok)
	assert.Equal(t, sess.ID(), got.ID())
}

func TestArena_StopSession(t *testing.T) {
	a := newTestArena()
	require.NoError(t, a.Start())
	defer a.Stop()

	// Create a session with many prompts so it takes a while.
	sess, _ := a.StartSession(context.Background(), "s1", session.Config{
		Gremlins: []string{"latency"}, // latency gremlin has delays
		Prompts:  []string{"p1", "p2", "p3", "p4", "p5"},
	})

	// Try to stop it. It may already be completed for fast gremlins.
	_ = a.StopSession(context.Background(), sess.ID())
}
