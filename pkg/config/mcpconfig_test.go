package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMCPConfig_ClaudeDesktop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude_desktop_config.json")

	configJSON := `{
		"mcpServers": {
			"hubspot": {
				"command": "npx",
				"args": ["@hubspot/mcp-server"],
				"env": {"HUBSPOT_TOKEN": "hsat_xxx"}
			},
			"postgres": {
				"url": "http://localhost:5433/mcp"
			}
		},
		"otherSettings": {
			"key": "value"
		}
	}`
	require.NoError(t, os.WriteFile(path, []byte(configJSON), 0o644))

	cfg, err := parseMCPConfig(path, "Claude Desktop")
	require.NoError(t, err)
	assert.Equal(t, "Claude Desktop", cfg.ClientName)
	assert.Len(t, cfg.Servers, 2)

	hubspot := cfg.Servers["hubspot"]
	assert.Equal(t, "npx", hubspot.Command)
	assert.Equal(t, []string{"@hubspot/mcp-server"}, hubspot.Args)
	assert.Equal(t, "hsat_xxx", hubspot.Env["HUBSPOT_TOKEN"])

	postgres := cfg.Servers["postgres"]
	assert.Equal(t, "http://localhost:5433/mcp", postgres.URL)

	// Verify unknown fields are preserved.
	assert.Contains(t, cfg.rawData, "otherSettings")
}

func TestParseMCPConfig_NoServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0o644))

	_, err := parseMCPConfig(path, "test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no MCP servers")
}

func TestParseMCPConfig_FileNotFound(t *testing.T) {
	_, err := parseMCPConfig("/nonexistent/path.json", "test")
	assert.Error(t, err)
}

func TestRewriteForWrap(t *testing.T) {
	entry := MCPServerEntry{
		Command: "npx",
		Args:    []string{"@hubspot/mcp-server"},
		Env:     map[string]string{"TOKEN": "abc"},
	}

	rewritten := RewriteForWrap(entry, "gremlyn", "gremlyn.yaml")
	assert.Equal(t, "gremlyn", rewritten.Command)
	assert.Equal(t, []string{"wrap", "--config", "gremlyn.yaml", "--", "npx", "@hubspot/mcp-server"}, rewritten.Args)
	assert.Equal(t, map[string]string{"TOKEN": "abc"}, rewritten.Env)
}

func TestRewriteForProxy(t *testing.T) {
	rewritten := RewriteForProxy("localhost:9090", "postgres")
	assert.Equal(t, "http://localhost:9090/mcp/postgres", rewritten.URL)
}

func TestIsAlreadyWrapped(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    bool
	}{
		// Already wrapped — must not be rewritten a second time.
		{"bare", "gremlyn", true},
		{"unix absolute", "/usr/local/bin/gremlyn", true},
		{"windows absolute", `C:\bin\gremlyn.exe`, true},
		{"windows relative", `bin\gremlyn.exe`, true},
		{"exe on unix path", "/opt/gremlyn.exe", true},
		{"mixed case", "/usr/bin/Gremlyn", true},

		// Not wrapped — must be rewritten.
		{"npx", "npx", false},
		{"python", "python", false},
		{"empty", "", false},
		{"prefix only", "gremlynx", false},
		{"suffix only", "my-gremlyn-helper", false},
		{"gremlyn in a directory, not the binary", "/opt/gremlyn/bin/server", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsAlreadyWrapped(MCPServerEntry{Command: tt.command}))
		})
	}
}

func TestBackupAndWriteConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := `{"mcpServers":{"test":{"command":"echo"}},"extra":"data"}`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	// Backup.
	backupPath, err := BackupConfig(path)
	require.NoError(t, err)
	assert.FileExists(t, backupPath)

	backupData, readErr := os.ReadFile(backupPath)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(backupData))

	// Parse, modify, and write.
	cfg, err := parseMCPConfig(path, "test")
	require.NoError(t, err)

	cfg.Servers["test"] = RewriteForWrap(cfg.Servers["test"], "gremlyn", "gremlyn.yaml")

	err = WriteConfig(path, *cfg)
	require.NoError(t, err)

	// Read back and verify.
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	var parsed map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &parsed))

	// Unknown field "extra" should be preserved.
	assert.Contains(t, parsed, "extra")
	assert.Contains(t, parsed, "mcpServers")

	// Verify the rewrite.
	var servers map[string]MCPServerEntry
	require.NoError(t, json.Unmarshal(parsed["mcpServers"], &servers))
	assert.Equal(t, "gremlyn", servers["test"].Command)
}

func TestGenerateDefaultGremlynYAML(t *testing.T) {
	configs := []MCPClientConfig{
		{
			Servers: map[string]MCPServerEntry{
				"hubspot": {Command: "npx", Args: []string{"@hubspot/mcp"}},
				"db":      {URL: "http://localhost:5433/mcp"},
			},
		},
	}

	cfg := GenerateDefaultGremlynYAML(configs)
	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, "shield", cfg.Mode)
	assert.Len(t, cfg.Servers, 2)

	assert.Equal(t, "wrap", string(cfg.Servers["hubspot"].Mode))
	assert.Equal(t, "npx", cfg.Servers["hubspot"].Command)

	assert.Equal(t, "proxy", string(cfg.Servers["db"].Mode))
	assert.Equal(t, "http://localhost:5433/mcp", cfg.Servers["db"].Upstream)

	assert.NotEmpty(t, cfg.Global.RateLimit)
	assert.NotEmpty(t, cfg.Global.MaxPayloadSize)
}
