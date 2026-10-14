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
	env := fakeEnv(map[string]string{
		"TOKEN": "s3cret", "TENANT": "acme", "EMPTY": "", "NL": "a\nb",
		"GH_TOKEN": "gh", "GITHUB_TOKEN": "gh", "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "oidc",
	})
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
		{"one header under two spellings", map[string]string{"Authorization": "a", "authorization": "b"}, nil, "the same header"},
		{"the GitHub CLI token", map[string]string{"Authorization": "Bearer ${GH_TOKEN}"}, nil, "CI credential variable"},
		{"the workflow token", map[string]string{"Authorization": "Bearer ${GITHUB_TOKEN}"}, nil, "CI credential variable"},
		{"the OIDC request token", map[string]string{"X-Key": "${ACTIONS_ID_TOKEN_REQUEST_TOKEN}"}, nil, "CI credential variable"},
		{"a reserved name", map[string]string{"connection": "x"}, nil, "managed by the proxy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveHeaders(tt.in, env, nil)
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

	lower, err := parseHeaderFlags([]string{"x-tenant: acme"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"X-Tenant": "acme"}, lower)

	for _, bad := range [][]string{{"no-colon"}, {": value"}, {"X-A: 1", "X-A: 2"}, {"Authorization: a", "authorization: b"}} {
		_, err := parseHeaderFlags(bad)
		assert.Error(t, err, "%v", bad)
	}
}

func TestResolveUpstream_ReportsTheVariablesItRead(t *testing.T) {
	env := fakeEnv(map[string]string{"TOKEN": "s", "TENANT": "acme", "PATHKEY": "k"})
	got, err := resolveUpstream(&MCPServerConfig{
		URL:     "https://mcp.example.com/${PATHKEY}/mcp",
		Headers: map[string]string{"Authorization": "Bearer ${TOKEN}", "X-Tenant": "${TENANT}", "X-Static": "v"},
	}, env)
	require.NoError(t, err)
	assert.Equal(t, []string{"PATHKEY", "TENANT", "TOKEN"}, got.Vars)
	assert.Equal(t, "https://mcp.example.com/k/mcp", got.URL)
}

func TestScrubEnv(t *testing.T) {
	environ := []string{"PATH=/bin", "TOKEN=s", "TOKEN_OTHER=x", "HOME=/h", "TOKEN=dup"}
	assert.Equal(t, []string{"PATH=/bin", "TOKEN_OTHER=x", "HOME=/h"}, scrubEnv(environ, []string{"TOKEN"}))
	assert.Equal(t, environ, scrubEnv(environ, nil))
}

func TestCheckAgentEnv(t *testing.T) {
	require.NoError(t, checkAgentEnv(map[string]string{"OTHER": "1"}, []string{"TOKEN"}))
	err := checkAgentEnv(map[string]string{"TOKEN": "x"}, []string{"TOKEN"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent.env sets TOKEN")
}

func TestResolveURL_RefusesCICredentials(t *testing.T) {
	env := fakeEnv(map[string]string{"GITHUB_TOKEN": "gh"})
	_, err := resolveURL("https://evil.example/${GITHUB_TOKEN}", env, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CI credential variable")
}

func TestSameEnvName(t *testing.T) {
	tests := []struct {
		goos string
		a, b string
		want bool
	}{
		{"linux", "ACME_TOKEN", "ACME_TOKEN", true},
		{"linux", "acme_token", "ACME_TOKEN", false},
		{"darwin", "Acme_Token", "ACME_TOKEN", false},
		{"windows", "acme_token", "ACME_TOKEN", true},
		{"windows", "ACME_TOKEN", "ACME_TOKENS", false},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.a, func(t *testing.T) {
			assert.Equal(t, tt.want, sameEnvName(tt.goos, tt.a, tt.b))
		})
	}
}

func TestScrubEnvFor_WindowsNamesAreCaseInsensitive(t *testing.T) {
	environ := []string{"Path=C:\\bin", "ACME_TOKEN=s", "Other=x"}
	assert.Equal(t, []string{"Path=C:\\bin", "Other=x"}, scrubEnvFor("windows", environ, []string{"acme_token"}),
		"on Windows ${acme_token} resolves ACME_TOKEN, so the scrub must remove it too")
	assert.Equal(t, environ, scrubEnvFor("linux", environ, []string{"acme_token"}))

	require.Error(t, checkAgentEnvFor("windows", map[string]string{"Acme_Token": "x"}, []string{"ACME_TOKEN"}))
	require.NoError(t, checkAgentEnvFor("linux", map[string]string{"Acme_Token": "x"}, []string{"ACME_TOKEN"}))
}
