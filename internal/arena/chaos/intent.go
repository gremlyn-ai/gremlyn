package chaos

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
)

var (
	quotedPattern = regexp.MustCompile("\"([^\"\\n]{4,200})\"|`([^`\\n]{4,200})`|\u201c([^\u201d\\n]{4,200})\u201d")
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

func MarkerIn(text, marker string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], marker)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(marker)
		if (start == 0 || !wordByte(text[start-1]) || !wordByte(marker[0])) &&
			(end == len(text) || !wordByte(text[end]) || !wordByte(marker[len(marker)-1])) {
			return true
		}
		i = start + 1
	}
}

func wordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func (o *Observer) resultText(msg *protocol.Message, mctx *proxy.MessageContext) string {
	if len(o.markers) == 0 || mctx == nil || mctx.Direction != models.DirectionIncoming ||
		msg == nil || msg.Type != protocol.MessageTypeResponse || msg.Response == nil || len(msg.Response.Result) == 0 {
		return ""
	}
	return argumentsText(msg.Response.Result)
}

func (o *Observer) noteSeenLocked(text string) {
	if text == "" {
		return
	}
	for _, ms := range o.markers {
		for _, m := range ms {
			if !o.seen[m] && strings.Contains(text, m) {
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
		if !o.seen[m] && MarkerIn(argsText, m) {
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

func callDigest(tool string, args json.RawMessage) string {
	sum := sha256.Sum256([]byte(tool + "\x00" + canonicalArguments(args)))
	return hex.EncodeToString(sum[:])
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
