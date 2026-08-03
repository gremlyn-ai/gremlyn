package gremlins

import (
	"context"
	"encoding/json"
	"math/rand"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// CorruptionMode defines how the response data is corrupted.
type CorruptionMode string

const (
	// CorruptionModeMissingFields removes random fields from the response.
	CorruptionModeMissingFields CorruptionMode = "missing_fields"
	// CorruptionModeWrongTypes replaces values with wrong types.
	CorruptionModeWrongTypes CorruptionMode = "wrong_types"
	// CorruptionModeTruncated truncates the response JSON.
	CorruptionModeTruncated CorruptionMode = "truncated"
)

// CorruptionGremlin alters MCP server responses with missing fields,
// wrong types, or truncated JSON to test input validation and error recovery.
type CorruptionGremlin struct {
	// Mode controls the type of corruption applied.
	Mode CorruptionMode `json:"mode"`
	// Probability of injection (0.0–1.0).
	Probability float64 `json:"probability"`
}

// NewCorruptionGremlin creates a CorruptionGremlin with the given mode and probability.
func NewCorruptionGremlin(mode CorruptionMode, probability float64) *CorruptionGremlin {
	return &CorruptionGremlin{
		Mode:        mode,
		Probability: probability,
	}
}

// Name implements Gremlin.
func (g *CorruptionGremlin) Name() string { return "corruption" }

// Description implements Gremlin.
func (g *CorruptionGremlin) Description() string {
	return "Corrupts MCP server responses (missing fields, wrong types, truncated)"
}

// Inject corrupts the response result according to the configured mode.
func (g *CorruptionGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg.Response == nil || msg.Response.Result == nil {
		return msg, false, nil
	}

	if rand.Float64() >= g.Probability {
		return msg, false, nil
	}

	modified := *msg
	modResp := *msg.Response

	switch g.Mode {
	case CorruptionModeMissingFields:
		modResp.Result = corruptMissingFields(msg.Response.Result)
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

func corruptMissingFields(data json.RawMessage) json.RawMessage {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return data
	}

	for k := range obj {
		if rand.Float64() < 0.5 {
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
