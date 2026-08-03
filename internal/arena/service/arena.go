// Package service provides the main Arena service orchestration layer.
package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/internal/arena/session"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

// Arena is the main service that orchestrates chaos testing sessions.
type Arena struct {
	Registry *gremlins.Registry
	Manager  *session.Manager
	Runner   *session.Runner

	eventStore    session.EventStore
	eventNotifier session.EventNotifier
	logger        zerolog.Logger

	mu      sync.Mutex
	started bool
}

// New creates an Arena service.
func New(
	registry *gremlins.Registry,
	manager *session.Manager,
	runner *session.Runner,
	eventStore session.EventStore,
	notifier session.EventNotifier,
	logger zerolog.Logger,
) *Arena {
	return &Arena{
		Registry:      registry,
		Manager:       manager,
		Runner:        runner,
		eventStore:    eventStore,
		eventNotifier: notifier,
		logger:        logger,
	}
}

// Start initializes the Arena service.
func (a *Arena) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.started {
		return fmt.Errorf("arena already started")
	}
	a.started = true
	a.logger.Info().Msg("arena service started")
	return nil
}

// Stop shuts down the Arena service.
func (a *Arena) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = false
	a.logger.Info().Msg("arena service stopped")
}

// StartSession creates and runs a new chaos session in the background.
func (a *Arena) StartSession(ctx context.Context, serverID string, cfg session.Config) (*session.Session, error) {
	sess, err := a.Manager.Create(ctx, serverID, cfg)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	recorder := session.NewRecorder(sess.ID(), a.eventStore, a.eventNotifier)

	// Use a detached context so the goroutine survives after the HTTP response.
	go func() {
		runCtx := context.Background()
		if err := a.Runner.Run(runCtx, sess, recorder); err != nil {
			a.logger.Error().Err(err).
				Str("session_id", sess.ID()).
				Msg("session run failed")
		}
		a.Manager.Persist(runCtx, sess)
	}()

	return sess, nil
}

// StopSession cancels a running session.
func (a *Arena) StopSession(ctx context.Context, sessionID string) error {
	return a.Manager.Stop(ctx, sessionID)
}

// GetSession returns a session by ID.
func (a *Arena) GetSession(id string) (*session.Session, bool) {
	return a.Manager.Get(id)
}

// ListSessions returns all sessions.
func (a *Arena) ListSessions() []*session.Session {
	return a.Manager.List()
}

// GetSessionEvents returns the events for a session from the event store.
func (a *Arena) GetSessionEvents(ctx context.Context, sessionID string) ([]models.ArenaEvent, error) {
	if a.eventStore == nil {
		return nil, fmt.Errorf("no event store configured")
	}
	return a.eventStore.ListBySession(ctx, sessionID)
}

// ListGremlins returns all available gremlins.
func (a *Arena) ListGremlins() []gremlins.Gremlin {
	return a.Registry.List()
}
