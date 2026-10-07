package filestore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

var ErrNotFound = errors.New("session not found")

const (
	sessionFile = "session.json"
	eventsFile  = "events.jsonl"
)

type Store struct {
	root   string
	logger zerolog.Logger
	mu     sync.Mutex
}

func New(root string, logger zerolog.Logger) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("creating arena session dir %q: %w", root, err)
	}
	return &Store{root: root, logger: logger}, nil
}

func (s *Store) dir(id string) (string, error) {
	if id == "" || id == "." || id == ".." ||
		strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	return filepath.Join(s.root, id), nil
}

func (s *Store) InsertSession(_ context.Context, m models.ArenaSession) error {
	return s.writeSession(m)
}

func (s *Store) UpdateSession(_ context.Context, m models.ArenaSession) error {
	return s.writeSession(m)
}

func (s *Store) writeSession(m models.ArenaSession) error {
	dir, err := s.dir(m.ID)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling session %q: %w", m.ID, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating session dir %q: %w", dir, err)
	}

	tmp := filepath.Join(dir, sessionFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing session %q: %w", m.ID, err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, sessionFile)); err != nil {
		return fmt.Errorf("committing session %q: %w", m.ID, err)
	}
	return nil
}

func (s *Store) GetSession(_ context.Context, id string) (models.ArenaSession, error) {
	dir, err := s.dir(id)
	if err != nil {
		return models.ArenaSession{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(filepath.Join(dir, sessionFile))
	if errors.Is(err, os.ErrNotExist) {
		return models.ArenaSession{}, ErrNotFound
	}
	if err != nil {
		return models.ArenaSession{}, fmt.Errorf("reading session %q: %w", id, err)
	}

	var m models.ArenaSession
	if err := json.Unmarshal(data, &m); err != nil {
		return models.ArenaSession{}, fmt.Errorf("parsing session %q: %w", id, err)
	}
	return m, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]models.ArenaSession, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("listing arena sessions in %q: %w", s.root, err)
	}

	out := make([]models.ArenaSession, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.GetSession(ctx, e.Name())
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			s.logger.Warn().Err(err).Str("session_id", e.Name()).
				Msg("skipping unreadable arena session")
			continue
		}
		out = append(out, m)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out, nil
}

func (s *Store) InsertEvent(_ context.Context, e models.ArenaEvent) error {
	dir, err := s.dir(e.SessionID)
	if err != nil {
		return err
	}

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshalling event %q: %w", e.ID, err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating session dir %q: %w", dir, err)
	}

	f, err := os.OpenFile(filepath.Join(dir, eventsFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening events file for %q: %w", e.SessionID, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("appending event to %q: %w", e.SessionID, err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("syncing events for %q: %w", e.SessionID, err)
	}
	return nil
}

func (s *Store) RecordEvent(ctx context.Context, e models.ArenaEvent) error {
	return s.InsertEvent(ctx, e)
}

func (s *Store) ListBySession(_ context.Context, id string) ([]models.ArenaEvent, error) {
	dir, err := s.dir(id)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}

	data, err := os.ReadFile(filepath.Join(dir, eventsFile))
	if errors.Is(err, os.ErrNotExist) {
		return []models.ArenaEvent{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading events for %q: %w", id, err)
	}

	events := make([]models.ArenaEvent, 0)
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var e models.ArenaEvent
		decErr := dec.Decode(&e)
		if errors.Is(decErr, io.EOF) {
			break
		}
		if decErr != nil {
			s.logger.Warn().Err(decErr).Str("session_id", id).
				Msg("skipping truncated trailing event")
			break
		}
		events = append(events, e)
	}
	return events, nil
}
