package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

type FileEventSink struct {
	mu   sync.Mutex
	f    *os.File
	enc  *json.Encoder
	path string
}

func NewFileEventSink(path string) (*FileEventSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening event file %q: %w", path, err)
	}
	return &FileEventSink{f: f, enc: json.NewEncoder(f), path: path}, nil
}

func (s *FileEventSink) RecordEvent(_ context.Context, event models.ArenaEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.enc.Encode(event); err != nil {
		return fmt.Errorf("writing event to %q: %w", s.path, err)
	}

	if err := s.f.Sync(); err != nil {
		return fmt.Errorf("syncing %q: %w", s.path, err)
	}
	return nil
}

func (s *FileEventSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.f.Close(); err != nil {
		return fmt.Errorf("closing %q: %w", s.path, err)
	}
	return nil
}

type SessionSummary struct {
	Coverage      Coverage      `json:"coverage"`
	Gremlins      []string      `json:"gremlins"`
	Seed          int64         `json:"seed"`
	Intensity     string        `json:"intensity,omitempty"`
	Params        GremlinParams `json:"params,omitempty"`
	ServerCommand []string      `json:"server_command,omitempty"`
}

func (s SessionSummary) Replayable() bool {
	return s.Seed != 0 && len(s.Gremlins) > 0
}

func WriteSummary(path string, s SessionSummary) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling summary: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing summary %q: %w", path, err)
	}
	return nil
}

func ReadEvents(path string) (events []models.ArenaEvent, skipped int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("reading event file %q: %w", path, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var e models.ArenaEvent
		decErr := dec.Decode(&e)
		if errors.Is(decErr, io.EOF) {
			break
		}
		if decErr != nil {
			skipped++
			break
		}
		events = append(events, e)
	}
	if skipped > 0 && len(events) == 0 {
		return nil, skipped, fmt.Errorf("event file %q contained no complete events", path)
	}
	return events, skipped, nil
}

func ReadSummary(path string) (SessionSummary, error) {
	var s SessionSummary
	data, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("reading summary %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing summary %q: %w", path, err)
	}
	return s, nil
}
