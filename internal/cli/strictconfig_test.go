package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeConfig(t *testing.T, top, agent, scenario string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "arena.yaml")
	body := fmt.Sprintf(`agent:
  command: ["agent", "--mcp-config", "%s"]
%smcp_server:
  name: notes
  command: ["./notes"]
scenarios:
  - name: s1
    gremlins: [injection]
%s%s`, PlaceholderMCPConfig, agent, scenario, top)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadCIConfig_UnknownKeys(t *testing.T) {
	tests := []struct {
		name               string
		top, agent, sc     string
		wantErr            []string
		wantNoSuggestionOf string
	}{
		{name: "top-level typo", top: "threshold:\n  min_overall: 50\n",
			wantErr: []string{"line 9", "threshold: unknown key", `did you mean "thresholds"`}},
		{name: "scenario typo", sc: "    intesity: high\n",
			wantErr: []string{"line 9", "scenarios[0].intesity: unknown key", `did you mean "intensity"`}},
		{name: "scenario singular", sc: "    target_tool: [create_note]\n",
			wantErr: []string{"scenarios[0].target_tool", `did you mean "target_tools"`}},
		{name: "agent typo", agent: "  timout: 1m\n",
			wantErr: []string{"line 3", "agent.timout: unknown key", `did you mean "timeout"`}},
		{name: "param typo", sc: "    params:\n      injection_txt: hi\n",
			wantErr: []string{"line 10", "scenarios[0].params.injection_txt", `did you mean "injection_text"`}},
		{name: "free-form key points at meta", sc: "    measures: resistance\n",
			wantErr: []string{"scenarios[0].measures: unknown key", "meta:"}, wantNoSuggestionOf: "did you mean"},
		{name: "unknown dimension", top: "thresholds:\n  min_dimension:\n    injection: 80\n",
			wantErr: []string{"thresholds.min_dimension", `"injection"`, "injection_defense"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadCIConfig(writeConfig(t, tt.top, tt.agent, tt.sc))
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, want := range tt.wantErr {
				assert.Contains(t, err.Error(), want)
			}
			if tt.wantNoSuggestionOf != "" {
				assert.NotContains(t, err.Error(), tt.wantNoSuggestionOf)
			}
		})
	}
}

func TestLoadCIConfig_Meta(t *testing.T) {
	cfg, err := loadCIConfig(writeConfig(t,
		"meta:\n  owner: notes-team\n  tags: [nightly]\n", "",
		"    meta:\n      measures: resistance\n      ticket: 42\n      nested: {a: 1}\n"))
	require.NoError(t, err)
	assert.Equal(t, "notes-team", cfg.Meta["owner"])
	assert.Equal(t, "resistance", cfg.Scenarios[0].Meta["measures"])
	assert.Equal(t, 42, cfg.Scenarios[0].Meta["ticket"])

	_, err = loadCIConfig(writeConfig(t, "", "", "    meta:\n      nested: {1: a}\n"))
	require.Error(t, err, "meta ends up in the JSON report, so its keys must be strings")
	assert.Contains(t, err.Error(), "meta")
}

func TestLoadCIConfig_StrayParam(t *testing.T) {
	_, err := loadCIConfig(writeConfig(t, "", "", "    params:\n      payload_bytes: 1024\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `scenario "s1"`)
	assert.Contains(t, err.Error(), "payload_bytes set but no gremlin of this scenario reads it")
}

func TestShippedConfigsStillLoad(t *testing.T) {
	t.Setenv("ACME_MCP_TOKEN", "example")
	configs, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.yaml"))
	require.NoError(t, err)
	configs = append(configs, filepath.Join("..", "..", ".gremlyn", "arena.yaml"))
	require.GreaterOrEqual(t, len(configs), 3)
	for _, path := range configs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, err := loadCIConfig(path)
			require.NoError(t, err)
		})
	}

	reports, err := filepath.Glob(filepath.Join("..", "..", "docs", "demo", "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, reports)
	for _, path := range reports {
		t.Run(filepath.Base(path), func(t *testing.T) {
			r, err := readCIResult(path)
			require.NoError(t, err)
			assert.NotEmpty(t, r.Scenarios)
		})
	}
}

func TestSuggestKey(t *testing.T) {
	known := []string{"gremlins", "intensity", "name", "prompt", "seed", "target_tools", "window"}
	tests := []struct{ in, want string }{
		{"intesity", "intensity"},
		{"target_tool", "target_tools"},
		{"sed", "seed"},
		{"Prompt", "prompt"},
		{"target", "target_tools"},
		{"wind", "window"},
		{"na", ""},
		{"measures", ""},
		{"x", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, suggestKey(tt.in, known), tt.in)
	}
}
