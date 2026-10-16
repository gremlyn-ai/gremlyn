package cli

import (
	"fmt"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"gopkg.in/yaml.v3"
)

type targetTools []string

func (t *targetTools) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: target_tools is a list of tool names, got %s", node.Line, node.ShortTag())
	}
	out := make(targetTools, 0, len(node.Content))
	for i, item := range node.Content {
		if item.Kind == yaml.AliasNode && item.Alias != nil {
			item = item.Alias
		}
		if item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
			return fmt.Errorf("line %d: target_tools[%d] must be a tool name (a string), got %s", item.Line, i, item.ShortTag())
		}
		out = append(out, item.Value)
	}
	*t = out
	return nil
}

func targetFailures(ts []chaos.TargetResult, nothingInjected bool) []string {
	out := make([]string, 0, len(ts))
	for i := range ts {
		t := &ts[i]
		var msg string
		switch {
		case t.State == chaos.TargetNeverCalled && t.Listing == chaos.ListingNotListed:
			msg = fmt.Sprintf("target %q is not offered by the server: no tools/list the run saw lists it (check the name)", t.Name)
		case t.State == chaos.TargetNeverCalled:
			msg = fmt.Sprintf("target %q was never called, so no gremlin reached it", t.Name)
		case t.State == chaos.TargetNotInjected && nothingInjected:
			msg = fmt.Sprintf("target %q was called %d time(s), 0 injected: nothing was measured", t.Name, t.Called)
		default:
			continue
		}
		if t.ListedAs != "" {
			msg += fmt.Sprintf(": the server lists %q, and target_tools takes the server's tool name, not mcp__server__tool", t.ListedAs)
		}
		out = append(out, msg)
	}
	return out
}

func targetLine(t chaos.TargetResult) string {
	return fmt.Sprintf("%s %s (called %d, injected %d, scored %d, %s)", t.Name, t.State, t.Called, t.Injected, t.Scored, t.Listing)
}
