package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

// Store persists session state to a backing store.
type Store interface {
	InsertSession(ctx context.Context, s models.ArenaSession) error
	UpdateSession(ctx context.Context, s models.ArenaSession) error
	GetSession(ctx context.Context, id string) (models.ArenaSession, error)
	ListSessions(ctx context.Context) ([]models.ArenaSession, error)
}

// Config holds the configuration for a chaos session.
type Config struct {
	Gremlins  []string `json:"gremlins"`
	Intensity string   `json:"intensity"` // low, medium, high, custom
	Prompts   []string `json:"prompts"`
}

// Session represents a running or completed chaos testing session.
type Session struct {
	mu       sync.RWMutex
	model    models.ArenaSession
	config   Config
	cancelFn context.CancelFunc
}

// ID returns the session ID.
func (s *Session) ID() string { return s.model.ID }

// Status returns the current session status.
func (s *Session) Status() models.SessionStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.model.Status
}

// Model returns a copy of the underlying ArenaSession model.
func (s *Session) Model() models.ArenaSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.model
}

// SetStatus updates the session status.
func (s *Session) SetStatus(status models.SessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model.Status = status
}

// Complete marks the session as completed with results.
func (s *Session) Complete(results json.RawMessage, sent, survived, crashed int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model.Status = models.SessionStatusCompleted
	now := time.Now()
	s.model.CompletedAt = &now
	s.model.Results = results
	s.model.GremlinsSent = sent
	s.model.GremlinsSurvived = survived
	s.model.GremlinsCrashed = crashed
}

// Cancel marks the session as cancelled.
func (s *Session) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model.Status = models.SessionStatusCancelled
	now := time.Now()
	s.model.CompletedAt = &now
	if s.cancelFn != nil {
		s.cancelFn()
	}
}

// Manager handles session lifecycle — creation, lookup, and state transitions.
type Manager struct {
	store  Store
	logger zerolog.Logger

	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewManager creates a session Manager. Store is optional (nil = in-memory only).
func NewManager(store Store, logger zerolog.Logger) *Manager {
	return &Manager{
		store:    store,
		logger:   logger,
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session in Created state (not yet running).
func (m *Manager) Create(ctx context.Context, serverID string, cfg Config) (*Session, error) {
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal session config: %w", err)
	}

	now := time.Now()
	sess := &Session{
		config: cfg,
		model: models.ArenaSession{
			ID:        uuid.New().String(),
			ServerID:  serverID,
			Status:    models.SessionStatusRunning,
			Config:    cfgJSON,
			StartedAt: now,
		},
	}

	if m.store != nil {
		if err := m.store.InsertSession(ctx, sess.model); err != nil {
			return nil, fmt.Errorf("persist session: %w", err)
		}
	}

	m.mu.Lock()
	m.sessions[sess.model.ID] = sess
	m.mu.Unlock()

	m.logger.Info().
		Str("session_id", sess.model.ID).
		Str("server_id", serverID).
		Strs("gremlins", cfg.Gremlins).
		Msg("session created")

	return sess, nil
}

// Get returns a session by ID.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// List returns all sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

// Persist writes the current session state to the backing store.
func (m *Manager) Persist(ctx context.Context, sess *Session) {
	if m.store == nil {
		return
	}
	if err := m.store.UpdateSession(ctx, sess.Model()); err != nil {
		m.logger.Warn().Err(err).Str("session_id", sess.ID()).Msg("failed to persist session")
	}
}

// Stop cancels a running session.
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %q not found", id)
	}

	if sess.Status() != models.SessionStatusRunning {
		return fmt.Errorf("session %q is not running (status=%s)", id, sess.Status())
	}

	sess.Cancel()

	if m.store != nil {
		if err := m.store.UpdateSession(ctx, sess.Model()); err != nil {
			return fmt.Errorf("persist cancellation: %w", err)
		}
	}

	m.logger.Info().Str("session_id", id).Msg("session cancelled")
	return nil
}
