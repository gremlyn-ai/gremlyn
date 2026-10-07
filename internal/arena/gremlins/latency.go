package gremlins

import (
	"context"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type LatencyGremlin struct {
	MinDelayMs  int     `json:"min_delay_ms"`
	MaxDelayMs  int     `json:"max_delay_ms"`
	TargetTool  string  `json:"target_tool,omitempty"`
	Probability float64 `json:"probability"`
	rng         *rng
}

func NewLatencyGremlin(minMs, maxMs int, probability float64, opts ...Option) *LatencyGremlin {
	o := applyOptions(opts)
	return &LatencyGremlin{
		MinDelayMs:  minMs,
		MaxDelayMs:  maxMs,
		Probability: probability,
		rng:         newRNG(o.seed, "latency"),
	}
}
func (g *LatencyGremlin) Name() string { return "latency" }
func (g *LatencyGremlin) Description() string {
	return "Adds artificial delay to MCP server responses"
}

func (g *LatencyGremlin) Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg == nil {
		return nil, false, nil
	}

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
