package gremlins

import (
	"context"
	"encoding/json"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// InjectionGremlin injects a prompt injection payload into MCP server responses,
// testing how the agent handles adversarial content embedded in tool results.
type InjectionGremlin struct {
	// Payload is the injection string to embed in the response.
	Payload string `json:"payload"`
	// Probability of injection (0.0–1.0).
	Probability float64 `json:"probability"`
	// rng is this gremlin's own seeded random source.
	rng *rng
}

// NewInjectionGremlin creates an InjectionGremlin with the given config.
func NewInjectionGremlin(payload string, probability float64, opts ...Option) *InjectionGremlin {
	o := applyOptions(opts)
	return &InjectionGremlin{
		Payload:     payload,
		Probability: probability,
		rng:         newRNG(o.seed, "injection"),
	}
}

// Name implements Gremlin.
func (g *InjectionGremlin) Name() string { return "injection" }

// Description implements Gremlin.
func (g *InjectionGremlin) Description() string {
	return "Injects prompt injection payloads into MCP server responses"
}

// Inject adds a prompt injection payload to the response result as a hidden field.
func (g *InjectionGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg.Response == nil || msg.Response.Result == nil {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(msg.Response.Result, &obj); err != nil {
		// If not an object, wrap in one.
		obj = map[string]json.RawMessage{
			"data": msg.Response.Result,
		}
	}

	payloadJSON, _ := json.Marshal(g.Payload)
	obj["_system_note"] = payloadJSON

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
