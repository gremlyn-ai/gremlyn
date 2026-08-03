package gremlins

import (
	"context"
	"encoding/json"
	"math/rand"
	"sync"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// LoopGremlin replaces tool responses with "retry needed" messages,
// testing whether the agent implements circuit breakers and max-retry limits.
type LoopGremlin struct {
	// MaxLoops is the maximum number of retry-injections before stopping.
	MaxLoops int `json:"max_loops"`
	// Message is the retry message injected into responses.
	Message string `json:"message"`
	// Probability of injection (0.0–1.0).
	Probability float64 `json:"probability"`

	mu        sync.Mutex
	loopCount int
}

// NewLoopGremlin creates a LoopGremlin with the given config.
func NewLoopGremlin(maxLoops int, message string, probability float64) *LoopGremlin {
	if message == "" {
		message = "Action completed but needs verification. Please retry."
	}
	return &LoopGremlin{
		MaxLoops:    maxLoops,
		Message:     message,
		Probability: probability,
	}
}

// Name implements Gremlin.
func (g *LoopGremlin) Name() string { return "loop" }

// Description implements Gremlin.
func (g *LoopGremlin) Description() string {
	return "Replaces responses with retry messages to test circuit breaker implementation"
}

// Inject replaces the response result with a retry message.
// Stops injecting after MaxLoops injections to allow the session to complete.
func (g *LoopGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg.Response == nil {
		return msg, false, nil
	}

	if rand.Float64() >= g.Probability {
		return msg, false, nil
	}

	g.mu.Lock()
	g.loopCount++
	count := g.loopCount
	g.mu.Unlock()

	if count > g.MaxLoops {
		return msg, false, nil
	}

	modified := *msg
	modResp := *msg.Response
	retryResult, _ := json.Marshal(map[string]string{
		"status":  "needs_verification",
		"message": g.Message,
	})
	modResp.Result = retryResult
	modified.Response = &modResp

	return &modified, true, nil
}

// LoopCount returns the current number of loops injected (for testing/scoring).
func (g *LoopGremlin) LoopCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.loopCount
}

// Reset resets the loop counter.
func (g *LoopGremlin) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.loopCount = 0
}
