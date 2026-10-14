package chaos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

var AnnotationHints = []string{"readOnlyHint", "destructiveHint", "idempotentHint"}

func ValidateAnnotationOverrides(tools map[string]models.ToolAnnotations) error {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := validateOverride(name, tools[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateOverride(name string, a models.ToolAnnotations) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a tool name cannot be empty")
	}
	if a.OpenWorldHint != nil {
		return fmt.Errorf("%s: openWorldHint changes no score, so declaring it would do nothing; remove it", name)
	}
	if a.ReadOnlyHint == nil && a.DestructiveHint == nil && a.IdempotentHint == nil {
		return fmt.Errorf("%s: declare at least one of %s", name, strings.Join(AnnotationHints, ", "))
	}
	if a.ReadOnlyHint != nil && *a.ReadOnlyHint && (a.DestructiveHint != nil || a.IdempotentHint != nil) {
		return fmt.Errorf("%s: a read-only tool writes nothing, so destructiveHint and idempotentHint do not apply to it", name)
	}
	return nil
}

func ParseAnnotationOverrides(data []byte) (map[string]models.ToolAnnotations, error) {
	var tools map[string]models.ToolAnnotations
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&tools); err != nil {
		return nil, fmt.Errorf("decoding tool annotations: %w", err)
	}
	if dec.More() {
		return nil, errors.New("decoding tool annotations: trailing data after the object")
	}
	if err := ValidateAnnotationOverrides(tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func ConfigOverrides(tools models.ToolCatalog) map[string]models.ToolAnnotations {
	var out map[string]models.ToolAnnotations
	for name, rec := range tools {
		if rec.Source != models.AnnotationSourceConfig {
			continue
		}
		if out == nil {
			out = make(map[string]models.ToolAnnotations)
		}
		out[name] = rec.Annotations
	}
	return out
}
