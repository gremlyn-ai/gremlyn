package chaos

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
)

var (
	quotedPattern = regexp.MustCompile("\"([^\"\\n]{3,200})\"|`([^`\\n]{3,200})`|\u201c([^\u201d\\n]{3,200})\u201d")
	canaryPattern = regexp.MustCompile(`GRM-[0-9a-f]{8}`)
)

func PayloadMarkers(text string) []string {
	var out []string
	add := func(m string) {
		m = strings.ToLower(strings.TrimRight(m, ".,;:!?)"))
		if m != "" && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	for _, m := range linkPattern.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range emailPattern.FindAllString(text, -1) {
		add(m)
	}
	for _, m := range quotedPattern.FindAllStringSubmatch(text, -1) {
		add(m[1] + m[2] + m[3])
	}
	for _, m := range canaryPattern.FindAllString(text, -1) {
		add(m)
	}
	return out
}

func (o *Observer) noteSeenLocked(msg *protocol.Message, mctx *proxy.MessageContext) {
	if len(o.markers) == 0 || mctx == nil || mctx.Direction != models.DirectionIncoming ||
		msg == nil || msg.Type != protocol.MessageTypeResponse || msg.Response == nil || len(msg.Response.Result) == 0 {
		return
	}
	lower := bytes.ToLower(msg.Response.Result)
	for _, ms := range o.markers {
		for _, m := range ms {
			if !o.seen[m] && bytes.Contains(lower, []byte(m)) {
				o.seen[m] = true
			}
		}
	}
}

func (o *Observer) payloadInLocked(gremlin, argsText string) string {
	if argsText == "" {
		return ""
	}
	for _, m := range o.markers[gremlin] {
		if !o.seen[m] && strings.Contains(argsText, m) {
			return m
		}
	}
	return ""
}

func argumentsText(args json.RawMessage) string {
	var v any
	if json.Unmarshal(args, &v) != nil {
		return strings.ToLower(string(args))
	}
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			b.WriteString(strings.ToLower(x))
			b.WriteByte('\n')
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return b.String()
}

func canonicalArguments(args json.RawMessage) string {
	var v any
	if json.Unmarshal(args, &v) != nil {
		return string(args)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(args)
	}
	return string(out)
}
