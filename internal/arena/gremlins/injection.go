package gremlins

import (
	"context"
	"encoding/json"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type InjectionGremlin struct {
	Payload     string  `json:"payload"`
	Placement   string  `json:"placement,omitempty"`
	Probability float64 `json:"probability"`
	rng         *rng
}

const (
	PlacementField   = "field"
	PlacementContent = "content"
)

func NewInjectionGremlin(payload string, probability float64, opts ...Option) *InjectionGremlin {
	o := applyOptions(opts)
	return &InjectionGremlin{
		Payload:     payload,
		Probability: probability,
		rng:         newRNG(o.seed, "injection"),
	}
}
func (g *InjectionGremlin) Name() string { return "injection" }
func (g *InjectionGremlin) Description() string {
	return "Injects prompt injection payloads into MCP server responses"
}

func (g *InjectionGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg == nil {
		return nil, false, nil
	}

	if msg.Response == nil || msg.Response.Result == nil {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(msg.Response.Result, &obj); err != nil {
		obj = map[string]json.RawMessage{
			"data": msg.Response.Result,
		}
	}

	if g.Placement != PlacementContent || !appendTextPart(obj, g.Payload) {
		payloadJSON, _ := json.Marshal(g.Payload)
		obj["_system_note"] = payloadJSON
	}

	result, err := json.Marshal(obj)
	if err != nil {
		return msg, false, nil
	}

	modified := *msg
	modResp := *msg.Response
	modResp.Result = result
	modified.Response = &modResp
	return &modified, true, nil
}

func appendTextPart(obj map[string]json.RawMessage, text string) bool {
	var parts *[]json.RawMessage
	if json.Unmarshal(obj["content"], &parts) != nil || parts == nil {
		return false
	}
	part, err := json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{"text", text})
	if err != nil {
		return false
	}
	content, err := json.Marshal(append(*parts, part))
	if err != nil {
		return false
	}
	obj["content"] = content
	return true
}

func (g *InjectionGremlin) PlacementFor(original *protocol.Message) string {
	if g.Placement != PlacementContent || original == nil || original.Response == nil {
		return ""
	}
	var obj map[string]json.RawMessage
	var parts *[]json.RawMessage
	if json.Unmarshal(original.Response.Result, &obj) != nil || json.Unmarshal(obj["content"], &parts) != nil || parts == nil {
		return PlacementField
	}
	return PlacementContent
}
