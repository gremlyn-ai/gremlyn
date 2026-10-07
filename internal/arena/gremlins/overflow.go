package gremlins

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type OverflowGremlin struct {
	SizeBytes   int     `json:"size_bytes"`
	Probability float64 `json:"probability"`
	rng         *rng
}

func NewOverflowGremlin(sizeBytes int, probability float64, opts ...Option) *OverflowGremlin {
	o := applyOptions(opts)
	return &OverflowGremlin{
		SizeBytes:   sizeBytes,
		Probability: probability,
		rng:         newRNG(o.seed, "overflow"),
	}
}
func (g *OverflowGremlin) Name() string { return "overflow" }
func (g *OverflowGremlin) Description() string {
	return "Generates oversized response payloads to test agent memory and truncation handling"
}

func (g *OverflowGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg == nil {
		return nil, false, nil
	}

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
