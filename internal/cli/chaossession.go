package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/gremlyn-ai/gremlyn/internal/arena/storage/filestore"
	"github.com/gremlyn-ai/gremlyn/pkg/datadir"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

type chaosRecipe struct {
	Gremlins  []string            `json:"gremlins"`
	Seed      int64               `json:"seed"`
	Intensity string              `json:"intensity,omitempty"`
	Params    chaos.GremlinParams `json:"params,omitempty"`
	Server    []string            `json:"server_command,omitempty"`
}

type chaosSessionRecorder struct {
	store     *filestore.Store
	id        string
	startedAt time.Time
	logger    zerolog.Logger
}

func newChaosSessionRecorder(
	ctx context.Context,
	server []string,
	gremlinNames []string,
	seed int64,
	intensity string,
	params chaos.GremlinParams,
	logger zerolog.Logger,
) *chaosSessionRecorder {
	dataDir, err := datadir.Dir()
	if err != nil {
		logger.Warn().Err(err).Msg("resolving data dir; chaos session not recorded to history")
		return nil
	}
	store, err := filestore.New(filepath.Join(dataDir, "arena", "sessions"), logger)
	if err != nil {
		logger.Warn().Err(err).Msg("opening arena session history; run not recorded")
		return nil
	}

	id := uuid.New().String()
	now := time.Now()

	recipe := chaosRecipe{
		Gremlins:  gremlinNames,
		Seed:      seed,
		Intensity: intensity,
		Params:    params,
		Server:    server,
	}
	cfg, err := json.Marshal(recipe)
	if err != nil {
		logger.Warn().Err(err).Msg("marshalling chaos recipe; run not recorded")
		return nil
	}
	rec := &chaosSessionRecorder{store: store, id: id, startedAt: now, logger: logger}
	if err := store.InsertSession(ctx, models.ArenaSession{
		ID:        id,
		ServerID:  serverLabel(server),
		Status:    models.SessionStatusRunning,
		Config:    cfg,
		StartedAt: now,
	}); err != nil {
		logger.Warn().Err(err).Msg("writing initial session record; run not recorded")
		return nil
	}

	logger.Info().Str("session_id", id).Msg("recording chaos session to history")
	return rec
}

func (r *chaosSessionRecorder) RecordEvent(ctx context.Context, e models.ArenaEvent) error {
	e.SessionID = r.id
	return r.store.InsertEvent(ctx, e)
}

func (r *chaosSessionRecorder) finish(ctx context.Context, runErr error) {
	events, err := r.store.ListBySession(ctx, r.id)
	if err != nil {
		r.logger.Warn().Err(err).Str("session_id", r.id).Msg("reading events to finalize session")
		return
	}

	sess, err := r.store.GetSession(ctx, r.id)
	if err != nil {
		r.logger.Warn().Err(err).Str("session_id", r.id).Msg("reloading session to finalize")
		return
	}

	report := scoring.Score(events, nil)

	var sent, survived, crashed int
	for _, e := range events {
		sent++
		switch e.Outcome {
		case models.OutcomeSurvived:
			survived++
		case models.OutcomeCrashed:
			crashed++
		case models.OutcomeDegraded, models.OutcomeUnmeasured:
		}
	}

	results, err := json.Marshal(report)
	if err != nil {
		r.logger.Warn().Err(err).Str("session_id", r.id).Msg("marshalling session results")
		return
	}

	now := time.Now()
	sess.Status = models.SessionStatusCompleted
	if runErr != nil {
		sess.Status = models.SessionStatusFailed
	}
	sess.Results = results
	sess.GremlinsSent = sent
	sess.GremlinsSurvived = survived
	sess.GremlinsCrashed = crashed
	sess.CompletedAt = &now

	if err := r.store.UpdateSession(ctx, sess); err != nil {
		r.logger.Warn().Err(err).Str("session_id", r.id).Msg("writing final session record")
		return
	}

	r.logger.Info().
		Str("session_id", r.id).
		Int("overall", report.Overall).
		Str("grade", string(report.Grade)).
		Bool("measured", report.Measured).
		Msg("chaos session recorded")
}

type multiEventSink []chaos.EventSink

func (m multiEventSink) RecordEvent(ctx context.Context, e models.ArenaEvent) error {
	var firstErr error
	for _, s := range m {
		if err := s.RecordEvent(ctx, e); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func combineEventSinks(sinks []chaos.EventSink) chaos.EventSink {
	switch len(sinks) {
	case 0:
		return nil
	case 1:
		return sinks[0]
	default:
		return multiEventSink(sinks)
	}
}

func serverLabel(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	out := argv[0]
	for _, a := range argv[1:] {
		out += " " + a
	}
	return out
}
