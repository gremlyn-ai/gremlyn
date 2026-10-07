package gremlins

import (
	"context"
	"fmt"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type TimeoutGremlin struct {
	MinTimeoutMs int     `json:"min_timeout_ms"`
	MaxTimeoutMs int     `json:"max_timeout_ms"`
	Probability  float64 `json:"probability"`
	rng          *rng
}

func NewTimeoutGremlin(minMs, maxMs int, probability float64, opts ...Option) *TimeoutGremlin {
	o := applyOptions(opts)
	return &TimeoutGremlin{
		MinTimeoutMs: minMs,
		MaxTimeoutMs: maxMs,
		Probability:  probability,
		rng:          newRNG(o.seed, "timeout"),
	}
}
func (g *TimeoutGremlin) Name() string { return "timeout" }
func (g *TimeoutGremlin) Description() string {
	return "Returns a JSON-RPC timeout error after a delay, as if the server timed out"
}

const jsonRPCTimeoutCode = -32001

func (g *TimeoutGremlin) Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg == nil {
		return nil, false, nil
	}

	if msg.Response == nil {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	timeoutRange := g.MaxTimeoutMs - g.MinTimeoutMs
	if timeoutRange <= 0 {
		timeoutRange = 1
	}
	timeout := time.Duration(g.MinTimeoutMs+g.rng.Intn(timeoutRange)) * time.Millisecond

	select {
	case <-time.After(timeout):
	case <-ctx.Done():
		return msg, false, ctx.Err()
	}

	modified := *msg
	resp := *msg.Response
	resp.Result = nil
	resp.Error = &protocol.JSONRPCError{
		Code:    jsonRPCTimeoutCode,
		Message: fmt.Sprintf("server timeout after %s", timeout),
	}
	modified.Response = &resp
	return &modified, true, nil
}
