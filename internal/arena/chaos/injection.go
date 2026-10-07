package chaos

import (
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type Injection struct {
	ID          string            `json:"id"`
	GremlinName string            `json:"gremlin_name"`
	Direction   models.Direction  `json:"direction"`
	RequestID   string            `json:"request_id,omitempty"`
	Method      string            `json:"method,omitempty"`
	Tool        string            `json:"tool,omitempty"`
	InjectedAt  time.Time         `json:"injected_at"`
	Original    *protocol.Message `json:"-"`
	Modified    *protocol.Message `json:"-"`
}

type InjectionSink interface {
	RecordInjection(inj Injection)
}

type InjectionLog struct {
	mu         sync.Mutex
	injections []Injection
}

func NewInjectionLog() *InjectionLog {
	return &InjectionLog{}
}

func (l *InjectionLog) RecordInjection(inj Injection) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.injections = append(l.injections, inj)
}

func (l *InjectionLog) Injections() []Injection {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Injection, len(l.injections))
	copy(out, l.injections)
	return out
}

func (l *InjectionLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.injections)
}
