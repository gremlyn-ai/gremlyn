package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Production scenario: Create session with production config
// ---------------------------------------------------------------------------

func TestSessionScenarios_CreateWithProductionConfig(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	// Simulate a real production CRM agent chaos test.
	cfg := Config{
		Gremlins:  []string{"hallucination", "latency", "corruption", "loop"},
		Intensity: "high",
		Prompts: []string{
			"Search for recent customer orders from Acme Corp",
			"Get contact details for john@acme.com",
			"List all open support tickets for account 12345",
			"Update the deal stage for opportunity OPP-789",
			"Generate a quarterly revenue report for Q1 2026",
		},
	}

	sess, err := m.Create(context.Background(), "hubspot-crm", cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID())
	assert.Equal(t, models.SessionStatusRunning, sess.Status())

	model := sess.Model()
	assert.Equal(t, "hubspot-crm", model.ServerID)
	assert.NotNil(t, model.Config)

	// Config should be serialized correctly.
	var parsed Config
	require.NoError(t, json.Unmarshal(model.Config, &parsed))
	assert.Len(t, parsed.Gremlins, 4)
	assert.Equal(t, "high", parsed.Intensity)
	assert.Len(t, parsed.Prompts, 5)
}

// ---------------------------------------------------------------------------
// Production scenario: Session status transitions
// ---------------------------------------------------------------------------

func TestSessionScenarios_StatusTransitions(t *testing.T) {
	logger := zerolog.Nop()
	m := NewManager(nil, logger)

	sess, _ := m.Create(context.Background(), "postgres-db", Config{
		Gremlins: []string{"corruption", "latency"},
		Prompts:  []string{"SELECT * FROM products"},
	})

	// Running (set at creation).
	assert.Equal(t, models.SessionStatusRunning, sess.Status())

	// Complete with production-like results.
	results := scoring.ResilienceReport{
		Overall: 72,
		Grade:   scoring.GradeGood,
		Dimensions: map[scoring.Dimension]scoring.DimensionScore{
			scoring.DimensionCorruption: {Score: 45, Total: 5, Survived: 2, Degraded: 2, Crashed: 1},
			scoring.DimensionLatency:    {Score: 94, Total: 5, Survived: 5},
		},
	}
	resultsJSON, _ := json.Marshal(results)
	sess.Complete(resultsJSON, 10, 7, 3)

	assert.Equal(t, models.SessionStatusCompleted, sess.Status())
	assert.NotNil(t, sess.Model().CompletedAt)
	assert.Equal(t, 10, sess.Model().GremlinsSent)
	assert.Equal(t, 7, sess.Model().GremlinsSurvived)
	assert.Equal(t, 3, sess.Model().GremlinsCrashed)

	// Verify results are correctly stored.
	var stored scoring.ResilienceReport
	require.NoError(t, json.Unmarshal(sess.Model().Results, &stored))
	assert.Equal(t, 72, stored.Overall)
	assert.Equal(t, scoring.GradeGood, stored.Grade)
}

// ---------------------------------------------------------------------------
// Production scenario: Session cancellation during run
// ---------------------------------------------------------------------------

func TestSessionScenarios_CancellationDuringRun(t *testing.T) {
	registry := gremlins.NewRegistry()
	registry.Register(gremlins.NewLatencyGremlin(1, 2, 1.0))

	logger := zerolog.Nop()
	runner := NewRunner(registry, logger)
	manager := NewManager(nil, logger)

	sess, err := manager.Create(context.Background(), "slack-messaging", Config{
		Gremlins: []string{"latency"},
		Prompts:  []string{"Send status update to #engineering"},
	})
	require.NoError(t, err)

	// Run the session normally first.
	rec := NewRecorder(sess.ID(), nil, nil)
	err = runner.Run(context.Background(), sess, rec)
	require.NoError(t, err)
	assert.Equal(t, models.SessionStatusCompleted, sess.Status())

	// Create a new session and cancel it via manager.
	sess2, _ := manager.Create(context.Background(), "slack-messaging", Config{
		Gremlins: []string{"latency"},
		Prompts:  []string{"another prompt"},
	})
	err = manager.Stop(context.Background(), sess2.ID())
	require.NoError(t, err)
	assert.Equal(t, models.SessionStatusCancelled, sess2.Status())
	assert.NotNil(t, sess2.Model().CompletedAt)
}

// ---------------------------------------------------------------------------
// Production scenario: Full CRM agent chaos run with all gremlins
// ---------------------------------------------------------------------------

func TestSessionScenarios_FullCRMChaosRun(t *testing.T) {
	registry := gremlins.NewRegistry()
	registry.Register(gremlins.NewHallucinationGremlin("delete_all_contacts", 1.0))
	registry.Register(gremlins.NewLatencyGremlin(1, 2, 1.0))
	registry.Register(gremlins.NewCorruptionGremlin(gremlins.CorruptionModeMissingFields, 1.0))
	registry.Register(gremlins.NewLoopGremlin(2, "CRM API rate limited, retry", 1.0))

	logger := zerolog.Nop()
	runner := NewRunner(registry, logger)
	manager := NewManager(nil, logger)

	sess, err := manager.Create(context.Background(), "hubspot-crm", Config{
		Gremlins:  []string{"hallucination", "latency", "corruption", "loop"},
		Intensity: "high",
		Prompts: []string{
			"Search for contacts at Acme Corp",
			"Get deal pipeline for Q1",
		},
	})
	require.NoError(t, err)

	rec := NewRecorder(sess.ID(), nil, nil)
	err = runner.Run(context.Background(), sess, rec)
	require.NoError(t, err)

	// Session completed.
	assert.Equal(t, models.SessionStatusCompleted, sess.Status())

	// Events were recorded.
	events := rec.Events()
	assert.Greater(t, len(events), 0)

	// Verify all gremlin types produced events.
	gremlinTypes := make(map[string]bool)
	for _, e := range events {
		gremlinTypes[e.GremlinType] = true
	}
	assert.True(t, gremlinTypes["hallucination"], "hallucination events expected")
	assert.True(t, gremlinTypes["latency"], "latency events expected")
	assert.True(t, gremlinTypes["corruption"], "corruption events expected")
	assert.True(t, gremlinTypes["loop"], "loop events expected")

	// Results contain valid resilience report.
	var report scoring.ResilienceReport
	require.NoError(t, json.Unmarshal(sess.Model().Results, &report))
	assert.Greater(t, report.Overall, 0)
	assert.NotEmpty(t, report.Grade)
	assert.NotEmpty(t, report.Dimensions)

	// Session model stats are set.
	model := sess.Model()
	assert.Greater(t, model.GremlinsSent, 0)
}

// ---------------------------------------------------------------------------
// Production scenario: Session with real-time notification
// ---------------------------------------------------------------------------

func TestSessionScenarios_RealTimeNotification(t *testing.T) {
	registry := gremlins.NewRegistry()
	registry.Register(gremlins.NewHallucinationGremlin("fake_tool", 1.0))

	logger := zerolog.Nop()
	runner := NewRunner(registry, logger)
	manager := NewManager(nil, logger)

	var notifiedEvents []models.ArenaEvent
	notifier := &mockNotifier{fn: func(e models.ArenaEvent) {
		notifiedEvents = append(notifiedEvents, e)
	}}

	sess, _ := manager.Create(context.Background(), "test-server", Config{
		Gremlins: []string{"hallucination"},
		Prompts:  []string{"test prompt"},
	})

	rec := NewRecorder(sess.ID(), nil, notifier)
	err := runner.Run(context.Background(), sess, rec)
	require.NoError(t, err)

	// Notifier should have received events in real-time.
	assert.Greater(t, len(notifiedEvents), 0)
	assert.Equal(t, "hallucination", notifiedEvents[0].GremlinType)
}
