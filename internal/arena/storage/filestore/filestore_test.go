package filestore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir(), zerolog.Nop())
	require.NoError(t, err)
	return s
}

func sampleSession(id string, started time.Time) models.ArenaSession {
	return models.ArenaSession{
		ID:        id,
		ServerID:  "memory",
		Status:    models.SessionStatusCompleted,
		Config:    json.RawMessage(`{"gremlins":["timeout"],"seed":42}`),
		Results:   json.RawMessage(`{"overall":90}`),
		StartedAt: started,
	}
}

func TestInsertAndGetSession_RoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	want := sampleSession("s-1", time.Unix(100, 0).UTC())

	require.NoError(t, s.InsertSession(ctx, want))

	got, err := s.GetSession(ctx, "s-1")
	require.NoError(t, err)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.ServerID, got.ServerID)
	assert.Equal(t, want.Status, got.Status)
	assert.JSONEq(t, string(want.Config), string(got.Config))
	assert.JSONEq(t, string(want.Results), string(got.Results))
	assert.True(t, want.StartedAt.Equal(got.StartedAt))
}

func TestGetSession_MissingIsErrNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.GetSession(context.Background(), "nope")
	assert.ErrorIs(t, err, ErrNotFound,
		"a missing session must be distinguishable from a read failure")
}

func TestUpdateSession_Overwrites(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.InsertSession(ctx, sampleSession("s-1", time.Unix(100, 0))))

	updated := sampleSession("s-1", time.Unix(100, 0))
	updated.Status = models.SessionStatusFailed
	require.NoError(t, s.UpdateSession(ctx, updated))

	got, err := s.GetSession(ctx, "s-1")
	require.NoError(t, err)
	assert.Equal(t, models.SessionStatusFailed, got.Status)
}

func TestListSessions_NewestFirstAndSkipsIncomplete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.InsertSession(ctx, sampleSession("old", time.Unix(100, 0))))
	require.NoError(t, s.InsertSession(ctx, sampleSession("new", time.Unix(300, 0))))
	require.NoError(t, s.InsertSession(ctx, sampleSession("mid", time.Unix(200, 0))))

	require.NoError(t, os.MkdirAll(filepath.Join(s.root, "half-written"), 0o700))

	got, err := s.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"new", "mid", "old"}, []string{got[0].ID, got[1].ID, got[2].ID})
}

func TestListSessions_EmptyRootIsEmptyNotError(t *testing.T) {
	s := newStore(t)
	got, err := s.ListSessions(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestListSessions_SkipsCorruptRecord(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.InsertSession(ctx, sampleSession("good", time.Unix(100, 0))))

	bad := filepath.Join(s.root, "bad")
	require.NoError(t, os.MkdirAll(bad, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(bad, sessionFile), []byte("{not json"), 0o600))

	got, err := s.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "good", got[0].ID)
}

func TestInsertEventAndListBySession_RoundTrip(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.InsertSession(ctx, sampleSession("s-1", time.Unix(100, 0))))
	e1 := models.ArenaEvent{ID: "e1", SessionID: "s-1", GremlinType: "timeout", Outcome: models.OutcomeSurvived, Score: 90}
	e2 := models.ArenaEvent{ID: "e2", SessionID: "s-1", GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 10}
	require.NoError(t, s.InsertEvent(ctx, e1))
	require.NoError(t, s.InsertEvent(ctx, e2))

	got, err := s.ListBySession(ctx, "s-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "e1", got[0].ID, "events come back in write order")
	assert.Equal(t, "e2", got[1].ID)
	assert.Equal(t, models.OutcomeCrashed, got[1].Outcome)
}

func TestListBySession_MissingSessionIsErrNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.ListBySession(context.Background(), "nope")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestListBySession_ExistingSessionNoEventsIsEmpty(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.InsertSession(ctx, sampleSession("s-1", time.Unix(100, 0))))

	got, err := s.ListBySession(ctx, "s-1")
	require.NoError(t, err)
	assert.Empty(t, got, "a recorded session with no events yet is empty, not not-found")
}

func TestListBySession_ToleratesTruncatedTail(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.InsertSession(ctx, sampleSession("s-1", time.Unix(100, 0))))
	require.NoError(t, s.InsertEvent(ctx, models.ArenaEvent{ID: "e1", SessionID: "s-1", GremlinType: "timeout"}))
	f, err := os.OpenFile(filepath.Join(s.root, "s-1", eventsFile), os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"id":"e2","gremlin_ty`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	got, err := s.ListBySession(ctx, "s-1")
	require.NoError(t, err)
	require.Len(t, got, 1, "the complete event survives; the truncated tail is dropped")
	assert.Equal(t, "e1", got[0].ID)
}

func TestRecordEvent_IsInsertEvent(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	require.NoError(t, s.InsertSession(ctx, sampleSession("s-1", time.Unix(100, 0))))
	require.NoError(t, s.RecordEvent(ctx, models.ArenaEvent{ID: "e1", SessionID: "s-1"}))
	got, err := s.ListBySession(ctx, "s-1")
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestPathTraversalRejected(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	for _, id := range []string{"", "..", "../escape", "a/b", `a\b`, "foo/../bar"} {
		err := s.InsertSession(ctx, models.ArenaSession{ID: id})
		assert.Errorf(t, err, "id %q must be rejected", id)
		_, gerr := s.GetSession(ctx, id)
		assert.Errorf(t, gerr, "id %q must be rejected on read", id)
	}
}
