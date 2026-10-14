package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"gopkg.in/yaml.v3"
)

type annotationOverrides map[string]models.ToolAnnotations

func (a *annotationOverrides) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: mcp_server.annotations maps each tool name to its hints", node.Line)
	}
	known := append(slices.Clone(chaos.AnnotationHints), "openWorldHint")
	out := make(annotationOverrides, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, body := node.Content[i], node.Content[i+1]
		if _, dup := out[name.Value]; dup {
			return fmt.Errorf("line %d: mcp_server.annotations: tool %q is declared twice", name.Line, name.Value)
		}
		if body.Kind != yaml.MappingNode {
			return fmt.Errorf("line %d: mcp_server.annotations.%s must map hints to true or false", body.Line, name.Value)
		}
		for j := 0; j+1 < len(body.Content); j += 2 {
			if key := body.Content[j]; !slices.Contains(known, key.Value) {
				return fmt.Errorf("line %d: mcp_server.annotations.%s: unknown hint %q (want %s)",
					key.Line, name.Value, key.Value, strings.Join(chaos.AnnotationHints, ", "))
			}
		}
		var ann models.ToolAnnotations
		if err := body.Decode(&ann); err != nil {
			return fmt.Errorf("mcp_server.annotations.%s: %w", name.Value, err)
		}
		out[name.Value] = ann
	}
	*a = out
	return nil
}
