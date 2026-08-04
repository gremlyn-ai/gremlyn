// Package chaos wires Arena's gremlins into the shared proxy pipeline, so that
// failures are injected into real MCP traffic rather than into fabricated
// messages.
package chaos

import (
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// Injection records that a gremlin fired on a message.
//
// It deliberately carries no outcome. What the agent *did* about the injection is
// not knowable at injection time — it is only visible in the traffic that follows,
// which the observer resolves afterwards. Recording a guessed outcome here is how
// Arena ended up with resilience scores that were a function of the enabled
// gremlin set rather than of the agent under test.
type Injection struct {
	// ID uniquely identifies this injection so a later observation can be
	// matched back to it.
	ID string `json:"id"`
	// GremlinName is the registry name of the gremlin that fired.
	GremlinName string `json:"gremlin_name"`
	// Direction is the direction of the message that was altered.
	Direction models.Direction `json:"direction"`
	// RequestID is the JSON-RPC id of the affected message, when it has one.
	// This is the correlation key for observing the agent's reaction.
	RequestID string `json:"request_id,omitempty"`
	// Method is the JSON-RPC method of the affected message, or of the request
	// it responds to, when known.
	Method string `json:"method,omitempty"`
	// InjectedAt is when the gremlin fired.
	InjectedAt time.Time `json:"injected_at"`
	// Original is the message as it arrived, before the gremlin touched it.
	Original *protocol.Message `json:"-"`
	// Modified is the message as forwarded.
	Modified *protocol.Message `json:"-"`
}

// InjectionSink receives injections as they happen.
//
// The observer implements this to correlate each injection with what the agent
// does next. It is intentionally a one-method interface owned by this package:
// the handler must not know how outcomes are decided.
type InjectionSink interface {
	RecordInjection(inj Injection)
}

// InjectionLog is an in-memory InjectionSink, used when no observer is attached
// and by tests.
type InjectionLog struct {
	mu         sync.Mutex
	injections []Injection
}

// NewInjectionLog returns an empty InjectionLog.
func NewInjectionLog() *InjectionLog {
	return &InjectionLog{}
}

// RecordInjection appends an injection to the log.
func (l *InjectionLog) RecordInjection(inj Injection) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.injections = append(l.injections, inj)
}

// Injections returns a copy of everything recorded so far.
func (l *InjectionLog) Injections() []Injection {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Injection, len(l.injections))
	copy(out, l.injections)
	return out
}

// Len returns how many injections have been recorded.
func (l *InjectionLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.injections)
}
