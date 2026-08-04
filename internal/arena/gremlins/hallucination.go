package gremlins

import (
	"context"
	"encoding/json"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// HallucinationGremlin replaces the tool name in an outgoing tool call with a
// non-existent tool, testing how the agent handles unknown-tool errors.
type HallucinationGremlin struct {
	// FakeToolName is the non-existent tool to substitute.
	FakeToolName string `json:"fake_tool_name"`
	// Probability of injection (0.0–1.0).
	Probability float64 `json:"probability"`
	// rng is this gremlin's own seeded random source.
	rng *rng
}

// NewHallucinationGremlin creates a HallucinationGremlin with the given config.
func NewHallucinationGremlin(fakeToolName string, probability float64, opts ...Option) *HallucinationGremlin {
	o := applyOptions(opts)
	return &HallucinationGremlin{
		FakeToolName: fakeToolName,
		Probability:  probability,
		rng:          newRNG(o.seed, "hallucination"),
	}
}

// Name implements Gremlin.
func (g *HallucinationGremlin) Name() string { return "hallucination" }

// Description implements Gremlin.
func (g *HallucinationGremlin) Description() string {
	return "Replaces tool calls with calls to non-existent tools"
}

// Inject replaces the tool name in a tools/call request with FakeToolName.
func (g *HallucinationGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg.Request == nil || msg.Request.Method != string(protocol.MCPMethodToolsCall) {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Request.Params, &params); err != nil {
		return msg, false, nil
	}

	params.Name = g.FakeToolName
	newParams, err := json.Marshal(params)
	if err != nil {
		return msg, false, nil
	}

	modified := *msg
	modReq := *msg.Request
	modReq.Params = newParams
	modified.Request = &modReq
	return &modified, true, nil
}
