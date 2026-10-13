package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeEnv(vars map[string]string) envLookup {
	return func(name string) (string, bool) {
		v, ok := vars[name]
		return v, ok
	}
}

func TestResolveHeaders(t *testing.T) {
	env := fakeEnv(map[string]string{"TOKEN": "s3cret", "TENANT": "acme", "EMPTY": "", "NL": "a\nb"})
	tests := []struct {
		name    string
		in      map[string]string
		want    map[string]string
		wantErr string
	}{
		{"no headers", nil, nil, ""},
		{"a literal value", map[string]string{"X-Tenant": "acme"}, map[string]string{"X-Tenant": "acme"}, ""},
		{"one variable", map[string]string{"Authorization": "Bearer ${TOKEN}"}, map[string]string{"Authorization": "Bearer s3cret"}, ""},
		{"two variables", map[string]string{"X-Key": "${TENANT}:${TOKEN}"}, map[string]string{"X-Key": "acme:s3cret"}, ""},
		{"a bare $VAR is literal", map[string]string{"X-Key": "$TOKEN"}, map[string]string{"X-Key": "$TOKEN"}, ""},
		{"a missing variable", map[string]string{"Authorization": "Bearer ${MISSING}"}, nil, "MISSING is not set"},
		{"an empty variable", map[string]string{"Authorization": "Bearer ${EMPTY}"}, nil, "EMPTY is not set"},
		{"an empty value", map[string]string{"X-Key": " "}, nil, "empty value"},
		{"an unterminated reference", map[string]string{"X-Key": "${TOKEN"}, nil, "unterminated"},
		{"an invalid reference", map[string]string{"X-Key": "${1BAD}"}, nil, "invalid variable reference"},
		{"a line break smuggled through a variable", map[string]string{"X-Key": "${NL}"}, nil, "line break"},
		{"an invalid name", map[string]string{"Bad Name": "x"}, nil, "not a valid HTTP header name"},
		{"a reserved name", map[string]string{"connection": "x"}, nil, "managed by the proxy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveHeaders(tt.in, env)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.NotContains(t, err.Error(), "s3cret", "an error never echoes a secret")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseHeaderFlags(t *testing.T) {
	got, err := parseHeaderFlags([]string{"Authorization: Bearer ${TOKEN}", "X-A:b"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"Authorization": "Bearer ${TOKEN}", "X-A": "b"}, got)

	for _, bad := range [][]string{{"no-colon"}, {": value"}, {"X-A: 1", "X-A: 2"}} {
		_, err := parseHeaderFlags(bad)
		assert.Error(t, err, "%v", bad)
	}
}
