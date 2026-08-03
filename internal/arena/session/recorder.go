// Package session manages chaos testing session lifecycle, execution, and event recording.
package session

import (
	"context"
	"sync"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

// EventStore persists arena events to a backing store.
type EventStore interface {
	InsertEvent(ctx context.Context, event models.ArenaEvent) error
	ListBySession(ctx context.Context, sessionID string) ([]models.ArenaEvent, error)
}

// EventNotifier pushes real-time event notifications (e.g., WebSocket).
type EventNotifier interface {
	Notify(event models.ArenaEvent)
}

// Recorder captures arena events during a session and optionally persists
// them to a store and broadcasts them via a notifier.
type Recorder struct {
	sessionID string
	store     EventStore
	notifier  EventNotifier

	mu     sync.Mutex
	events []models.ArenaEvent
}

// NewRecorder creates a Recorder for the given session.
// Store and notifier are optional — pass nil to skip persistence/notification.
func NewRecorder(sessionID string, store EventStore, notifier EventNotifier) *Recorder {
	return &Recorder{
		sessionID: sessionID,
		store:     store,
		notifier:  notifier,
	}
}

// Record adds an event to the in-memory list, persists it, and notifies listeners.
func (r *Recorder) Record(ctx context.Context, event models.ArenaEvent) error {
	event.SessionID = r.sessionID

	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()

	if r.store != nil {
		if err := r.store.InsertEvent(ctx, event); err != nil {
			return err
		}
	}

	if r.notifier != nil {
		r.notifier.Notify(event)
	}

	return nil
}

// Events returns a copy of all recorded events.
func (r *Recorder) Events() []models.ArenaEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.ArenaEvent, len(r.events))
	copy(out, r.events)
	return out
}
