package chaos

import (
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateAnnotationOverrides(t *testing.T) {
	tests := []struct {
		name    string
		tools   map[string]models.ToolAnnotations
		wantErr string
	}{
		{"none", nil, ""},
		{"a non-idempotent write", map[string]models.ToolAnnotations{"create_note": {ReadOnlyHint: hint(false), IdempotentHint: hint(false)}}, ""},
		{"a destructive tool", map[string]models.ToolAnnotations{"delete_note": {DestructiveHint: hint(true)}}, ""},
		{"a read-only tool", map[string]models.ToolAnnotations{"list_notes": {ReadOnlyHint: hint(true)}}, ""},
		{"an empty name", map[string]models.ToolAnnotations{" ": {ReadOnlyHint: hint(true)}}, "tool name cannot be empty"},
		{"no hint declared", map[string]models.ToolAnnotations{"x": {}}, "declare at least one of readOnlyHint, destructiveHint, idempotentHint"},
		{"openWorldHint changes nothing", map[string]models.ToolAnnotations{"x": {ReadOnlyHint: hint(true), OpenWorldHint: hint(true)}}, "openWorldHint changes no score"},
		{"read-only and destructive", map[string]models.ToolAnnotations{"x": {ReadOnlyHint: hint(true), DestructiveHint: hint(true)}}, "a read-only tool writes nothing"},
		{"read-only and idempotency", map[string]models.ToolAnnotations{"x": {ReadOnlyHint: hint(true), IdempotentHint: hint(false)}}, "a read-only tool writes nothing"},
		{"the first bad tool by name is reported", map[string]models.ToolAnnotations{"b": {}, "a": {}}, "a: declare"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAnnotationOverrides(tt.tools)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestParseAnnotationOverrides(t *testing.T) {
	got, err := ParseAnnotationOverrides([]byte(`{"create_note":{"readOnlyHint":false,"idempotentHint":false}}`))
	require.NoError(t, err)
	assert.Equal(t, models.EffectNonIdempotentWrite, got["create_note"].Effect())

	for _, bad := range []string{
		`{"create_note":{"idempotent":false,"readOnlyHint":false}}`,
		`{"create_note":{"idempotentHint":"no"}}`,
		`{"create_note":{}}`,
		`{"a":{"readOnlyHint":true}} {}`,
		`[]`,
		``,
	} {
		_, err := ParseAnnotationOverrides([]byte(bad))
		assert.Error(t, err, "%q must be rejected", bad)
	}
}

func TestConfigOverrides(t *testing.T) {
	assert.Nil(t, ConfigOverrides(nil))
	assert.Nil(t, ConfigOverrides(models.ToolCatalog{"a": {Annotations: models.ToolAnnotations{ReadOnlyHint: hint(true)}, Source: models.AnnotationSourceServer}}))
	got := ConfigOverrides(models.ToolCatalog{
		"a": {Annotations: models.ToolAnnotations{ReadOnlyHint: hint(true)}, Source: models.AnnotationSourceServer},
		"b": {Annotations: models.ToolAnnotations{DestructiveHint: hint(true)}, Source: models.AnnotationSourceConfig},
	})
	assert.Equal(t, map[string]models.ToolAnnotations{"b": {DestructiveHint: hint(true)}}, got)
}
