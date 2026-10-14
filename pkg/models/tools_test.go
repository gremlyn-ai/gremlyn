package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func hint(v bool) *bool { return &v }

func TestToolAnnotations_Effect(t *testing.T) {
	tests := []struct {
		name string
		ann  ToolAnnotations
		want ToolEffect
	}{
		{"no annotations is unknown, not the spec defaults", ToolAnnotations{}, EffectUnknown},
		{"openWorldHint alone says nothing about writes", ToolAnnotations{OpenWorldHint: hint(true)}, EffectUnknown},
		{"read-only", ToolAnnotations{ReadOnlyHint: hint(true)}, EffectReadOnly},
		{"read-only wins over write hints", ToolAnnotations{ReadOnlyHint: hint(true), DestructiveHint: hint(true), IdempotentHint: hint(false)}, EffectReadOnly},
		{"declared write, nothing else", ToolAnnotations{ReadOnlyHint: hint(false)}, EffectWrite},
		{"declared idempotent write", ToolAnnotations{ReadOnlyHint: hint(false), DestructiveHint: hint(false), IdempotentHint: hint(true)}, EffectWrite},
		{"declared non-destructive only", ToolAnnotations{DestructiveHint: hint(false)}, EffectWrite},
		{"non-idempotent create", ToolAnnotations{ReadOnlyHint: hint(false), DestructiveHint: hint(false), IdempotentHint: hint(false)}, EffectNonIdempotentWrite},
		{"non-idempotent without readOnlyHint", ToolAnnotations{IdempotentHint: hint(false)}, EffectNonIdempotentWrite},
		{"destructive delete", ToolAnnotations{ReadOnlyHint: hint(false), DestructiveHint: hint(true), IdempotentHint: hint(true)}, EffectDestructive},
		{"destructive with idempotency undeclared", ToolAnnotations{DestructiveHint: hint(true)}, EffectDestructive},
		{"destructive and non-idempotent", ToolAnnotations{DestructiveHint: hint(true), IdempotentHint: hint(false)}, EffectNonIdempotentDestructive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.ann.Effect())
		})
	}
}

func TestToolEffect_Predicates(t *testing.T) {
	tests := []struct {
		effect        ToolEffect
		known         bool
		destructive   bool
		nonIdempotent bool
	}{
		{EffectUnknown, false, false, false},
		{EffectReadOnly, true, false, false},
		{EffectWrite, true, false, false},
		{EffectNonIdempotentWrite, true, false, true},
		{EffectDestructive, true, true, false},
		{EffectNonIdempotentDestructive, true, true, true},
		{ToolEffect("from-a-newer-binary"), false, false, false},
	}
	require.Len(t, AllToolEffects(), len(tests)-1)
	for _, tt := range tests {
		t.Run(string(tt.effect), func(t *testing.T) {
			assert.Equal(t, tt.known, tt.effect.Known())
			assert.Equal(t, tt.destructive, tt.effect.Destructive())
			assert.Equal(t, tt.nonIdempotent, tt.effect.NonIdempotentWrite())
		})
	}
}

func TestToolCatalog_Effect(t *testing.T) {
	c := ToolCatalog{
		"delete_note": {Annotations: ToolAnnotations{DestructiveHint: hint(true)}, Source: AnnotationSourceServer},
		"bare":        {Source: AnnotationSourceServer},
	}
	assert.Equal(t, EffectDestructive, c.Effect("delete_note"))
	assert.Equal(t, EffectUnknown, c.Effect("bare"))
	assert.Equal(t, EffectUnknown, c.Effect("never_listed"))
	assert.Equal(t, EffectUnknown, c.Effect(""))
	assert.Equal(t, EffectUnknown, ToolCatalog(nil).Effect("delete_note"))
}

func TestToolCatalog_Clone(t *testing.T) {
	assert.Nil(t, ToolCatalog(nil).Clone())
	assert.Nil(t, ToolCatalog{}.Clone())

	c := ToolCatalog{"a": {Source: AnnotationSourceConfig}}
	cp := c.Clone()
	cp["b"] = ToolRecord{Source: AnnotationSourceServer}
	assert.Len(t, c, 1, "a clone must not write through to the original")
}

func TestToolAnnotations_SpecFieldNames(t *testing.T) {
	var a ToolAnnotations
	require.NoError(t, json.Unmarshal([]byte(`{"title":"Delete","readOnlyHint":false,"destructiveHint":true,"idempotentHint":true,"openWorldHint":false}`), &a))
	assert.Equal(t, ToolAnnotations{ReadOnlyHint: hint(false), DestructiveHint: hint(true), IdempotentHint: hint(true), OpenWorldHint: hint(false)}, a)
	assert.True(t, a.Declared())
	assert.False(t, ToolAnnotations{}.Declared())

	var y ToolAnnotations
	require.NoError(t, yaml.Unmarshal([]byte("idempotentHint: false\nreadOnlyHint: false\n"), &y))
	assert.Equal(t, EffectNonIdempotentWrite, y.Effect())
}

func TestEventDetails_KeepsTheLegacyKeys(t *testing.T) {
	data, err := json.Marshal(EventDetails{Gremlin: "timeout", Reaction: "retried", Tool: "read_graph", Method: "tools/call"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"gremlin":"timeout","reaction":"retried","tool":"read_graph","method":"tools/call"}`, string(data))
}
