package gremlins

import (
	"context"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// LatencyGremlin adds artificial delay to MCP server responses,
// testing how the agent handles slow tool execution and timeouts.
type LatencyGremlin struct {
	// MinDelayMs is the minimum delay in milliseconds.
	MinDelayMs int `json:"min_delay_ms"`
	// MaxDelayMs is the maximum delay in milliseconds.
	MaxDelayMs int `json:"max_delay_ms"`
	// TargetTool limits injection to a specific tool (empty = all tools).
	TargetTool string `json:"target_tool,omitempty"`
	// Probability of injection (0.0–1.0).
	Probability float64 `json:"probability"`
	// rng is this gremlin's own seeded random source.
	rng *rng
}

// NewLatencyGremlin creates a LatencyGremlin with the given config.
func NewLatencyGremlin(minMs, maxMs int, probability float64, opts ...Option) *LatencyGremlin {
	o := applyOptions(opts)
	return &LatencyGremlin{
		MinDelayMs:  minMs,
		MaxDelayMs:  maxMs,
		Probability: probability,
		rng:         newRNG(o.seed, "latency"),
	}
}

// Name implements Gremlin.
func (g *LatencyGremlin) Name() string { return "latency" }

// Description implements Gremlin.
func (g *LatencyGremlin) Description() string {
	return "Adds artificial delay to MCP server responses"
}

// Inject delays the response by a random duration between MinDelayMs and MaxDelayMs.
// The message itself is not modified — only its delivery is delayed.
func (g *LatencyGremlin) Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg.Response == nil {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	delayRange := g.MaxDelayMs - g.MinDelayMs
	if delayRange <= 0 {
		delayRange = 1
	}
	delay := time.Duration(g.MinDelayMs+g.rng.Intn(delayRange)) * time.Millisecond

	select {
	case <-time.After(delay):
		return msg, true, nil
	case <-ctx.Done():
		return msg, false, ctx.Err()
	}
}
