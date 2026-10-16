package cli

import (
	"fmt"

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
