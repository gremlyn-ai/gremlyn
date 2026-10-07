package gremlins

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type CorruptionMode string

const (
	CorruptionModeMissingFields CorruptionMode = "missing_fields"
	CorruptionModeWrongTypes    CorruptionMode = "wrong_types"
	CorruptionModeTruncated     CorruptionMode = "truncated"
)

type CorruptionGremlin struct {
	Mode        CorruptionMode `json:"mode"`
	Probability float64        `json:"probability"`
	rng         *rng
}

func NewCorruptionGremlin(mode CorruptionMode, probability float64, opts ...Option) *CorruptionGremlin {
	o := applyOptions(opts)
	return &CorruptionGremlin{
		Mode:        mode,
		Probability: probability,
		rng:         newRNG(o.seed, "corruption"),
	}
}
func (g *CorruptionGremlin) Name() string { return "corruption" }
func (g *CorruptionGremlin) Description() string {
	return "Corrupts MCP server responses (missing fields, wrong types, truncated)"
}

func (g *CorruptionGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg == nil {
		return nil, false, nil
	}

	if msg.Response == nil || msg.Response.Result == nil {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	modified := *msg
	modResp := *msg.Response

	switch g.Mode {
	case CorruptionModeMissingFields:
		modResp.Result = corruptMissingFields(msg.Response.Result, g.rng)
	case CorruptionModeWrongTypes:
		modResp.Result = corruptWrongTypes()
	case CorruptionModeTruncated:
		modResp.Result = corruptTruncated(msg.Response.Result)
	default:
		return msg, false, nil
	}

	modified.Response = &modResp
	return &modified, true, nil
}

func corruptMissingFields(data json.RawMessage, r *rng) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return data
	}

	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if r.Float64() < 0.5 {
			delete(obj, k)
		}
	}

	result, err := json.Marshal(obj)
	if err != nil {
		return data
	}
	return result
}

func corruptWrongTypes() json.RawMessage {
	return json.RawMessage(`{"error_type":42,"status":true,"data":"not_an_object"}`)
}

func corruptTruncated(data json.RawMessage) json.RawMessage {
	if len(data) > 10 {
		return data[:len(data)/2]
	}
	return data
}
