package gremlins

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

const (
	PlacementField   = "field"
	PlacementContent = "content"
)

type Placed struct {
	Placement string `json:"placement,omitempty"`
}

func (p *Placed) place(result json.RawMessage) json.RawMessage {
	if p.Placement != PlacementContent {
		return result
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(result, &obj) != nil || hasContent(obj) {
		return result
	}
	text, err := unescaped(result)
	if err != nil {
		return result
	}
	part, err := textPart(text)
	if err != nil {
		return result
	}
	return json.RawMessage(`{"content":[` + string(part) + `]}`)
}

func (p *Placed) PlacementFor(_, modified *protocol.Message) string {
	if p.Placement != PlacementContent || modified == nil || modified.Response == nil {
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(modified.Response.Result, &obj) != nil {
		return ""
	}
	if hasContent(obj) {
		return PlacementContent
	}
	return PlacementField
}

func unescaped(result json.RawMessage) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(result))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}

func hasContent(obj map[string]json.RawMessage) bool {
	var parts *[]json.RawMessage
	return json.Unmarshal(obj["content"], &parts) == nil && parts != nil
}

func textPart(text string) (json.RawMessage, error) {
	return json.Marshal(struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{"text", text})
}
