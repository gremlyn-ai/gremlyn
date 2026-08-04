package gremlins

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// OverflowGremlin generates a massive response payload to test how the agent
// handles oversized data — memory limits, truncation, and graceful degradation.
type OverflowGremlin struct {
	// SizeBytes is the target size of the overflow payload in bytes.
	SizeBytes int `json:"size_bytes"`
	// Probability of injection (0.0–1.0).
	Probability float64 `json:"probability"`
	// rng is this gremlin's own seeded random source.
	rng *rng
}

// NewOverflowGremlin creates an OverflowGremlin with the given config.
func NewOverflowGremlin(sizeBytes int, probability float64, opts ...Option) *OverflowGremlin {
	o := applyOptions(opts)
	return &OverflowGremlin{
		SizeBytes:   sizeBytes,
		Probability: probability,
		rng:         newRNG(o.seed, "overflow"),
	}
}

// Name implements Gremlin.
func (g *OverflowGremlin) Name() string { return "overflow" }

// Description implements Gremlin.
func (g *OverflowGremlin) Description() string {
	return "Generates oversized response payloads to test agent memory and truncation handling"
}

// Inject replaces the response with a massive JSON payload.
func (g *OverflowGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg.Response == nil || msg.Response.Result == nil {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	bigData := strings.Repeat("X", g.SizeBytes)
	payload := map[string]string{
		"data":   bigData,
		"status": "ok",
	}

	result, err := json.Marshal(payload)
	if err != nil {
		return msg, false, nil
	}

	modified := *msg
	modResp := *msg.Response
	modResp.Result = result
	modified.Response = &modResp
	return &modified, true, nil
}
