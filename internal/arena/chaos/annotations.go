package chaos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

func AnnotationHints() []string {
	return []string{"readOnlyHint", "destructiveHint", "idempotentHint"}
}

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
		return fmt.Errorf("%s: declare at least one of %s", name, strings.Join(AnnotationHints(), ", "))
	}
	if a.ReadOnlyHint != nil && *a.ReadOnlyHint && (a.DestructiveHint != nil || a.IdempotentHint != nil) {
		return fmt.Errorf("%s: a read-only tool writes nothing, so destructiveHint and idempotentHint do not apply to it", name)
	}
	return nil
}

func ParseAnnotationOverrides(data []byte) (map[string]models.ToolAnnotations, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw map[string]map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding tool annotations: %w", err)
	}
	if raw == nil {
		return nil, errors.New("tool annotations must be a JSON object mapping each tool name to its hints")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("decoding tool annotations: trailing data after the object")
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := make(map[string]models.ToolAnnotations, len(raw))
	for _, name := range names {
		ann, err := parseHints(name, raw[name])
		if err != nil {
			return nil, err
		}
		tools[name] = ann
	}
	if err := ValidateAnnotationOverrides(tools); err != nil {
		return nil, err
	}
	return tools, nil
}

func IsAnnotationHint(key string) bool {
	return key == "openWorldHint" || slices.Contains(AnnotationHints(), key)
}

func parseHints(name string, hints map[string]json.RawMessage) (models.ToolAnnotations, error) {
	var ann models.ToolAnnotations
	if hints == nil {
		return ann, fmt.Errorf("%s: declare at least one of %s", name, strings.Join(AnnotationHints(), ", "))
	}
	keys := make([]string, 0, len(hints))
	for key := range hints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !IsAnnotationHint(key) {
			return ann, fmt.Errorf("%s: unknown hint %q (want %s)", name, key, strings.Join(AnnotationHints(), ", "))
		}
		var v *bool
		if err := json.Unmarshal(hints[key], &v); err != nil || v == nil {
			return ann, fmt.Errorf("%s: %s must be true or false", name, key)
		}
		switch key {
		case "readOnlyHint":
			ann.ReadOnlyHint = v
		case "destructiveHint":
			ann.DestructiveHint = v
		case "idempotentHint":
			ann.IdempotentHint = v
		default:
			ann.OpenWorldHint = v
		}
	}
	return ann, nil
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
