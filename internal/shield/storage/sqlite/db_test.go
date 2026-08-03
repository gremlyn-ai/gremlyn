package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(":memory:", zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// ── Events ──

func TestEventsRepo_InsertAndList(t *testing.T) {
	db := newTestDB(t)
	repo := db.Events()
	ctx := context.Background()

	event := &models.Event{
		ServerID:    "srv-1",
		Direction:   "outgoing",
		MessageType: "tool_call",
		ToolName:    "read_file",
		ActionTaken: "allowed",
		LatencyMS:   42,
	}

	id, err := repo.Insert(ctx, event)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	events, err := repo.ListByServer(ctx, "srv-1", 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, id, events[0].ID)
	assert.Equal(t, "read_file", events[0].ToolName)
	assert.Equal(t, 42, events[0].LatencyMS)
}

func TestEventsRepo_ListBlocked(t *testing.T) {
	db := newTestDB(t)
	repo := db.Events()
	ctx := context.Background()

	_, err := repo.Insert(ctx, &models.Event{
		ServerID: "srv-1", Direction: "outgoing", MessageType: "tool_call",
		ActionTaken: "blocked", LatencyMS: 1,
	})
	require.NoError(t, err)

	_, err = repo.Insert(ctx, &models.Event{
		ServerID: "srv-1", Direction: "outgoing", MessageType: "tool_call",
		ActionTaken: "allowed", LatencyMS: 1,
	})
	require.NoError(t, err)

	blocked, err := repo.ListBlocked(ctx, 10, 0)
	require.NoError(t, err)
	assert.Len(t, blocked, 1)
	assert.Equal(t, models.ActionTaken("blocked"), blocked[0].ActionTaken)
}

func TestEventsRepo_CountByAction(t *testing.T) {
	db := newTestDB(t)
	repo := db.Events()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := repo.Insert(ctx, &models.Event{
			ServerID: "s", Direction: "outgoing", MessageType: "m",
			ActionTaken: "blocked", LatencyMS: 0,
		})
		require.NoError(t, err)
	}

	counts, err := repo.CountByAction(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, counts["blocked"])
}

// ── Rules ──

func TestRulesRepo_CRUD(t *testing.T) {
	db := newTestDB(t)
	repo := db.Rules()
	ctx := context.Background()

	rule := &models.Rule{
		ServerID: "srv-1",
		Name:     "block-dangerous",
		Action:   "block",
		Source:   "manual",
		Enabled:  true,
	}

	id, err := repo.Insert(ctx, rule)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "block-dangerous", got.Name)
	assert.True(t, got.Enabled)

	got.Name = "block-renamed"
	got.Enabled = false
	require.NoError(t, repo.Update(ctx, got))

	got2, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "block-renamed", got2.Name)
	assert.False(t, got2.Enabled)

	require.NoError(t, repo.Delete(ctx, id))
	_, err = repo.GetByID(ctx, id)
	assert.Error(t, err)
}

func TestRulesRepo_ListEnabled(t *testing.T) {
	db := newTestDB(t)
	repo := db.Rules()
	ctx := context.Background()

	_, err := repo.Insert(ctx, &models.Rule{
		ServerID: "s", Name: "enabled-rule", Action: "block", Source: "manual", Enabled: true,
	})
	require.NoError(t, err)

	_, err = repo.Insert(ctx, &models.Rule{
		ServerID: "s", Name: "disabled-rule", Action: "allow", Source: "manual", Enabled: true,
	})
	require.NoError(t, err)

	// Disable the second one.
	rules, err := repo.ListByServer(ctx, "s")
	require.NoError(t, err)
	require.Len(t, rules, 2)
	rules[1].Enabled = false
	require.NoError(t, repo.Update(ctx, &rules[1]))

	enabled, err := repo.ListEnabled(ctx)
	require.NoError(t, err)
	assert.Len(t, enabled, 1)
	assert.Equal(t, "enabled-rule", enabled[0].Name)
}

// ── Alerts ──

func TestAlertsRepo_InsertAndList(t *testing.T) {
	db := newTestDB(t)
	events := db.Events()
	alerts := db.Alerts()
	ctx := context.Background()

	// Need an event first for FK.
	eventID, err := events.Insert(ctx, &models.Event{
		ServerID: "s", Direction: "outgoing", MessageType: "m",
		ActionTaken: "blocked", LatencyMS: 0,
	})
	require.NoError(t, err)

	id, err := alerts.Insert(ctx, &models.Alert{
		EventID:  eventID,
		Severity: "critical",
		Type:     "prompt_injection",
		Message:  "Injection detected",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	list, err := alerts.ListRecent(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, models.AlertStatusNew, list[0].Status)
	assert.Nil(t, list[0].ResolvedAt)
}

func TestAlertsRepo_UpdateStatus(t *testing.T) {
	db := newTestDB(t)
	events := db.Events()
	alerts := db.Alerts()
	ctx := context.Background()

	eventID, err := events.Insert(ctx, &models.Event{
		ServerID: "s", Direction: "outgoing", MessageType: "m",
		ActionTaken: "blocked", LatencyMS: 0,
	})
	require.NoError(t, err)

	id, err := alerts.Insert(ctx, &models.Alert{
		EventID: eventID, Severity: "high", Type: "pii", Message: "PII found",
	})
	require.NoError(t, err)

	require.NoError(t, alerts.UpdateStatus(ctx, id, models.AlertStatusResolved))

	list, err := alerts.ListRecent(ctx, 10, 0)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, models.AlertStatusResolved, list[0].Status)
	assert.NotNil(t, list[0].ResolvedAt)
}

func TestAlertsRepo_CountBySeverity(t *testing.T) {
	db := newTestDB(t)
	events := db.Events()
	alerts := db.Alerts()
	ctx := context.Background()

	eventID, err := events.Insert(ctx, &models.Event{
		ServerID: "s", Direction: "outgoing", MessageType: "m",
		ActionTaken: "blocked", LatencyMS: 0,
	})
	require.NoError(t, err)

	for _, sev := range []models.AlertSeverity{"critical", "critical", "high"} {
		_, err := alerts.Insert(ctx, &models.Alert{
			EventID: eventID, Severity: sev, Type: "t", Message: "m",
		})
		require.NoError(t, err)
	}

	counts, err := alerts.CountBySeverity(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, counts["critical"])
	assert.Equal(t, 1, counts["high"])
}

// ── Migration idempotency ──

func TestMigrationIdempotent(t *testing.T) {
	db := newTestDB(t)
	// Running migrate again should be a no-op.
	require.NoError(t, db.migrate())
}

// ── JSON round-trip ──

func TestEventsRepo_JSONFields(t *testing.T) {
	db := newTestDB(t)
	repo := db.Events()
	ctx := context.Background()

	toolArgs := json.RawMessage(`{"path":"/etc/passwd"}`)
	event := &models.Event{
		ServerID:         "srv-1",
		Direction:        "outgoing",
		MessageType:      "tool_call",
		ToolArgs:         toolArgs,
		ActionTaken:      "blocked",
		RulesTriggered:   []string{"rule-1", "rule-2"},
		DetectionResults: map[string]string{"regex": "high"},
		LatencyMS:        10,
	}

	id, err := repo.Insert(ctx, event)
	require.NoError(t, err)

	events, err := repo.ListByServer(ctx, "srv-1", 10, 0)
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, id, events[0].ID)
	assert.JSONEq(t, `{"path":"/etc/passwd"}`, string(events[0].ToolArgs))
	assert.Equal(t, []string{"rule-1", "rule-2"}, events[0].RulesTriggered)
	assert.Equal(t, "high", events[0].DetectionResults["regex"])
}

// ── Time round-trip ──

func TestTimeRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	s := formatTime(now)
	parsed, err := parseTime(s)
	require.NoError(t, err)
	assert.True(t, now.Equal(parsed))
}

func TestNullableTimeRoundTrip(t *testing.T) {
	// nil case
	got, err := parseNullableTime(nil)
	require.NoError(t, err)
	assert.Nil(t, got)

	// non-nil case
	now := time.Now().UTC()
	s := formatTime(now)
	got, err = parseNullableTime(&s)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, now.Equal(*got))
}
