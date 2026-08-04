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

// FileEventSink appends resolved arena events to a JSON Lines file.
//
// This is the IPC between a chaos-enabled `gremlyn wrap` and whoever is scoring
// the session. In stdio MCP the *agent* spawns its own server, so the gremlins
// have to live inside the process the agent launches — which means the scorer is
// in a different process and needs a channel back.
//
// A file is deliberately the whole mechanism: it needs no daemon, works
// identically in CI and on a laptop, survives the agent being killed, and is a
// readable artifact a CI job can upload. One JSON object per line, so a partial
// last line from a killed process costs at most one event.
type FileEventSink struct {
	mu   sync.Mutex
	f    *os.File
	enc  *json.Encoder
	path string
}

// NewFileEventSink opens (creating or truncating) the given path for writing.
func NewFileEventSink(path string) (*FileEventSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening event file %q: %w", path, err)
	}
	return &FileEventSink{f: f, enc: json.NewEncoder(f), path: path}, nil
}

// RecordEvent appends one event. It implements EventSink.
func (s *FileEventSink) RecordEvent(_ context.Context, event models.ArenaEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.enc.Encode(event); err != nil {
		return fmt.Errorf("writing event to %q: %w", s.path, err)
	}
	// Flush per event: the agent may be killed at any moment, and an event that
	// only exists in a buffer is an observation we silently lost.
	if err := s.f.Sync(); err != nil {
		return fmt.Errorf("syncing %q: %w", s.path, err)
	}
	return nil
}

// Close closes the underlying file.
func (s *FileEventSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.f.Close(); err != nil {
		return fmt.Errorf("closing %q: %w", s.path, err)
	}
	return nil
}

// SessionSummary is what a chaos-enabled wrap process reports about its run,
// alongside the events themselves.
type SessionSummary struct {
	Coverage Coverage `json:"coverage"`
	Gremlins []string `json:"gremlins"`
	Seed     int64    `json:"seed"`
}

// WriteSummary writes a session summary as JSON to path.
//
// Coverage cannot be derived from the event stream alone: an injection that was
// never observed produces no event, so without this a reader could not tell a
// fully-measured session from a truncated one.
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

// ReadEvents reads a JSON Lines event file.
//
// A trailing partial line is skipped rather than failing the whole read: the
// writer may have been killed mid-event, and losing one observation is better
// than discarding every observation that preceded it. The count of skipped lines
// is returned so the caller can report the loss instead of hiding it.
func ReadEvents(path string) (events []models.ArenaEvent, skipped int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("reading event file %q: %w", path, err)
	}

	// json.Decoder reads a stream of whitespace-separated values, so it handles
	// JSON Lines without any line splitting of our own.
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var e models.ArenaEvent
		decErr := dec.Decode(&e)
		if errors.Is(decErr, io.EOF) {
			break // clean end of file
		}
		if decErr != nil {
			skipped++
			break // a truncated tail: everything before it is still good
		}
		events = append(events, e)
	}
	if skipped > 0 && len(events) == 0 {
		return nil, skipped, fmt.Errorf("event file %q contained no complete events", path)
	}
	return events, skipped, nil
}

// ReadSummary reads a session summary written by WriteSummary.
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
