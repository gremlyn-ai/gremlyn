package gremlins

import (
	"context"
	"fmt"
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
	// rng is this gremlin's own seeded random source.
	rng *rng
}

// NewTimeoutGremlin creates a TimeoutGremlin with the given config.
func NewTimeoutGremlin(minMs, maxMs int, probability float64, opts ...Option) *TimeoutGremlin {
	o := applyOptions(opts)
	return &TimeoutGremlin{
		MinTimeoutMs: minMs,
		MaxTimeoutMs: maxMs,
		Probability:  probability,
		rng:          newRNG(o.seed, "timeout"),
	}
}

// Name implements Gremlin.
func (g *TimeoutGremlin) Name() string { return "timeout" }

// Description implements Gremlin.
func (g *TimeoutGremlin) Description() string {
	return "Simulates complete server timeouts — response never arrives"
}

// jsonRPCTimeoutCode is the error code reported for a simulated server timeout.
// -32000 to -32099 is the JSON-RPC implementation-defined server-error range.
const jsonRPCTimeoutCode = -32001

// Inject waits, then replaces the response with a timeout error.
//
// The timeout is expressed as a JSON-RPC error response, which is what a client
// actually sees when a server stops answering — not as a Go error. A Go error
// from Inject means the gremlin itself malfunctioned, and the pipeline handler
// correctly passes the message through untouched in that case. Returning one here
// meant the injection was swallowed and this gremlin did nothing at all.
func (g *TimeoutGremlin) Inject(ctx context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
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
		// The session was cancelled while we were stalling. That is an abort, not
		// an observation, so leave the message alone.
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
