package gremlins

import (
	"context"
	"encoding/json"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type HallucinationGremlin struct {
	FakeToolName string  `json:"fake_tool_name"`
	Probability  float64 `json:"probability"`
	rng          *rng
}

func NewHallucinationGremlin(fakeToolName string, probability float64, opts ...Option) *HallucinationGremlin {
	o := applyOptions(opts)
	return &HallucinationGremlin{
		FakeToolName: fakeToolName,
		Probability:  probability,
		rng:          newRNG(o.seed, "hallucination"),
	}
}
func (g *HallucinationGremlin) Name() string { return "hallucination" }
func (g *HallucinationGremlin) Description() string {
	return "Replaces tool calls with calls to non-existent tools"
}

func (g *HallucinationGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg == nil {
		return nil, false, nil
	}

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
