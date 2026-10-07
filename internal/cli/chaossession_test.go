package cli

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/storage/filestore"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readBackStore(t *testing.T, dataDir string) *filestore.Store {
	t.Helper()
	s, err := filestore.New(filepath.Join(dataDir, "arena", "sessions"), zerolog.Nop())
	require.NoError(t, err)
	return s
}

func TestChaosSessionRecorder_RecordsCompletedScoredSession(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("GREMLYN_DATA_DIR", dataDir)
	ctx := context.Background()

	rec := newChaosSessionRecorder(ctx,
		[]string{"npx", "server-memory"},
		[]string{"timeout"}, 42, "certain", chaos.GremlinParams{}, zerolog.Nop())
	require.NotNil(t, rec)

	require.NoError(t, rec.RecordEvent(ctx, models.ArenaEvent{
		ID: "e1", GremlinType: "timeout", Outcome: models.OutcomeSurvived, Score: 90,
	}))
	require.NoError(t, rec.RecordEvent(ctx, models.ArenaEvent{
		ID: "e2", GremlinType: "timeout", Outcome: models.OutcomeCrashed, Score: 10,
	}))

	rec.finish(ctx, nil)

	store := readBackStore(t, dataDir)
	sessions, err := store.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1)

	got := sessions[0]
	assert.Equal(t, models.SessionStatusCompleted, got.Status)
	assert.Equal(t, "npx server-memory", got.ServerID, "the wrapped server is recorded")
	assert.Equal(t, 2, got.GremlinsSent)
	assert.Equal(t, 1, got.GremlinsSurvived)
	assert.Equal(t, 1, got.GremlinsCrashed)
	require.NotNil(t, got.CompletedAt)
	assert.NotEmpty(t, got.Config, "the replay recipe is stored as config")
	assert.NotEmpty(t, got.Results, "the score report is stored as results")

	events, err := store.ListBySession(ctx, got.ID)
	require.NoError(t, err)
	assert.Len(t, events, 2)
}

func TestChaosSessionRecorder_FailedRun(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("GREMLYN_DATA_DIR", dataDir)
	ctx := context.Background()

	rec := newChaosSessionRecorder(ctx,
		[]string{"srv"}, []string{"timeout"}, 1, "", chaos.GremlinParams{}, zerolog.Nop())
	require.NotNil(t, rec)

	rec.finish(ctx, assert.AnError)

	store := readBackStore(t, dataDir)
	sessions, err := store.ListSessions(ctx)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, models.SessionStatusFailed, sessions[0].Status)
}

func TestCombineEventSinks_FansOut(t *testing.T) {
	ctx := context.Background()
	a, b := &countingSink{}, &countingSink{}
	require.Nil(t, combineEventSinks(nil), "no sinks means no recording")
	single := combineEventSinks([]chaos.EventSink{a})
	require.NoError(t, single.RecordEvent(ctx, models.ArenaEvent{ID: "x"}))
	assert.Equal(t, 1, a.n)
	both := combineEventSinks([]chaos.EventSink{a, b})
	require.NoError(t, both.RecordEvent(ctx, models.ArenaEvent{ID: "y"}))
	assert.Equal(t, 2, a.n)
	assert.Equal(t, 1, b.n)
}

type countingSink struct{ n int }

func (c *countingSink) RecordEvent(_ context.Context, _ models.ArenaEvent) error {
	c.n++
	return nil
}
