package sqlite

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
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

// ── Sessions ──

func TestSessionsRepo_InsertAndGet(t *testing.T) {
	db := newTestDB(t)
	repo := db.Sessions()
	ctx := context.Background()

	cfgJSON, _ := json.Marshal(map[string]interface{}{"gremlins": []string{"latency"}})
	sess := models.ArenaSession{
		ID:        uuid.New().String(),
		ServerID:  "srv-1",
		Status:    models.SessionStatusRunning,
		Config:    cfgJSON,
		StartedAt: time.Now().UTC(),
	}

	require.NoError(t, repo.InsertSession(ctx, sess))

	got, err := repo.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, sess.ID, got.ID)
	assert.Equal(t, "srv-1", got.ServerID)
	assert.Equal(t, models.SessionStatusRunning, got.Status)
	assert.JSONEq(t, string(cfgJSON), string(got.Config))
	assert.Nil(t, got.CompletedAt)
}

func TestSessionsRepo_Update(t *testing.T) {
	db := newTestDB(t)
	repo := db.Sessions()
	ctx := context.Background()

	sess := models.ArenaSession{
		ID:        uuid.New().String(),
		ServerID:  "srv-1",
		Status:    models.SessionStatusRunning,
		Config:    json.RawMessage(`{}`),
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.InsertSession(ctx, sess))

	now := time.Now().UTC()
	sess.Status = models.SessionStatusCompleted
	sess.CompletedAt = &now
	sess.GremlinsSent = 10
	sess.GremlinsSurvived = 7
	sess.GremlinsCrashed = 3
	sess.Results = json.RawMessage(`{"score":70}`)

	require.NoError(t, repo.UpdateSession(ctx, sess))

	got, err := repo.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, models.SessionStatusCompleted, got.Status)
	assert.NotNil(t, got.CompletedAt)
	assert.Equal(t, 10, got.GremlinsSent)
	assert.Equal(t, 7, got.GremlinsSurvived)
	assert.Equal(t, 3, got.GremlinsCrashed)
	assert.JSONEq(t, `{"score":70}`, string(got.Results))
}

func TestSessionsRepo_List(t *testing.T) {
	db := newTestDB(t)
	repo := db.Sessions()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.InsertSession(ctx, models.ArenaSession{
			ID:        uuid.New().String(),
			ServerID:  "srv-1",
			Status:    models.SessionStatusRunning,
			Config:    json.RawMessage(`{}`),
			StartedAt: time.Now().UTC(),
		}))
	}

	sessions, err := repo.ListSessions(ctx)
	require.NoError(t, err)
	assert.Len(t, sessions, 3)
}

// ── Events ──

func TestEventsRepo_InsertAndList(t *testing.T) {
	db := newTestDB(t)
	sessions := db.Sessions()
	events := db.Events()
	ctx := context.Background()

	sessID := uuid.New().String()
	require.NoError(t, sessions.InsertSession(ctx, models.ArenaSession{
		ID:        sessID,
		ServerID:  "srv-1",
		Status:    models.SessionStatusRunning,
		Config:    json.RawMessage(`{}`),
		StartedAt: time.Now().UTC(),
	}))

	event := models.ArenaEvent{
		ID:            uuid.New().String(),
		SessionID:     sessID,
		GremlinType:   "latency",
		GremlinConfig: json.RawMessage(`{"min_delay_ms":2000}`),
		InjectedAt:    time.Now().UTC(),
		Outcome:       "survived",
		Score:         85,
	}

	require.NoError(t, events.InsertEvent(ctx, event))

	got, err := events.ListBySession(ctx, sessID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "latency", got[0].GremlinType)
	assert.Equal(t, 85, got[0].Score)
	assert.JSONEq(t, `{"min_delay_ms":2000}`, string(got[0].GremlinConfig))
}

func TestEventsRepo_EmptySession(t *testing.T) {
	db := newTestDB(t)
	sessions := db.Sessions()
	events := db.Events()
	ctx := context.Background()

	sessID := uuid.New().String()
	require.NoError(t, sessions.InsertSession(ctx, models.ArenaSession{
		ID:        sessID,
		ServerID:  "srv-1",
		Status:    models.SessionStatusRunning,
		Config:    json.RawMessage(`{}`),
		StartedAt: time.Now().UTC(),
	}))

	got, err := events.ListBySession(ctx, sessID)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// ── Migration idempotency ──

func TestMigrationIdempotent(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, db.migrate())
}

// ── Time helpers ──

func TestTimeRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	s := formatTime(now)
	parsed, err := parseTime(s)
	require.NoError(t, err)
	assert.True(t, now.Equal(parsed))
}

func TestNullableTimeRoundTrip(t *testing.T) {
	got, err := parseNullableTime(nil)
	require.NoError(t, err)
	assert.Nil(t, got)

	now := time.Now().UTC()
	s := formatTime(now)
	got, err = parseNullableTime(&s)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, now.Equal(*got))
}
