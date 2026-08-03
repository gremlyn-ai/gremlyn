package gremlins

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// TimeoutGremlin simulates a complete server timeout — the response never arrives.
// Unlike LatencyGremlin which delays, this gremlin blocks until the context is
// cancelled, testing how the agent handles total unresponsiveness.
type TimeoutGremlin struct {
	// MinTimeoutMs is the minimum wait before simulating timeout.
	MinTimeoutMs int `json:"min_timeout_ms"`
	// MaxTimeoutMs is the maximum wait before simulating timeout.
	MaxTimeoutMs int `json:"max_timeout_ms"`
	// Probability of injection (0.0–1.0).
	Probability float64 `json:"probability"`
}

// NewTimeoutGremlin creates a TimeoutGremlin with the given config.
func NewTimeoutGremlin(minMs, maxMs int, probability float64) *TimeoutGremlin {
	return &TimeoutGremlin{
		MinTimeoutMs: minMs,
		MaxTimeoutMs: maxMs,
		Probability:  probability,
	}
}

// Name implements Gremlin.
func (g *TimeoutGremlin) Name() string { return "timeout" }

// Description implements Gremlin.
func (g *TimeoutGremlin) Description() string {
	return "Simulates complete server timeouts — response never arrives"
}

// Inject blocks until the context is cancelled, simulating a server that never responds.
func (g *TimeoutGremlin) Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg.Response == nil {
		return msg, false, nil
	}

	if rand.Float64() >= g.Probability {
		return msg, false, nil
	}

	timeoutRange := g.MaxTimeoutMs - g.MinTimeoutMs
	if timeoutRange <= 0 {
		timeoutRange = 1
	}
	timeout := time.Duration(g.MinTimeoutMs+rand.Intn(timeoutRange)) * time.Millisecond

	select {
	case <-time.After(timeout):
		return msg, false, fmt.Errorf("server timeout after %s", timeout)
	case <-ctx.Done():
		return msg, false, ctx.Err()
	}
}
