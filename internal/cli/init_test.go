package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitCmd_NoConfigs(t *testing.T) {
	logger := zerolog.Nop()
	cmd := NewInitCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Point to a non-existent config so no auto-detection works.
	cmd.SetArgs([]string{"--config", "/nonexistent/path/mcp.json"})
	err := cmd.Execute()
	// Should fail because the config file doesn't exist.
	assert.Error(t, err)
}

func TestInitCmd_DryRun(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"mcpServers": {"test-server": {"command": "npx", "args": ["test-server"]}}
	}`), 0o644))

	logger := zerolog.Nop()
	cmd := NewInitCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", configPath, "--dry-run"})
	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "test-server")
	assert.Contains(t, output, "dry-run")
	assert.Contains(t, output, "gremlyn.yaml")
}

func TestInitCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"mcpServers": {"memory": {"command": "npx", "args": ["@modelcontextprotocol/server-memory"]}}
	}`), 0o644))

	logger := zerolog.Nop()
	cmd := NewInitCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", configPath, "--json"})
	err := cmd.Execute()
	require.NoError(t, err)

	var result initJSONOutput
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.TotalServers)
}

func TestInitCmd_GenerateFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"mcpServers": {"test": {"command": "echo", "args": ["hello"]}}
	}`), 0o644))

	outputPath := filepath.Join(dir, "gremlyn.yaml")

	logger := zerolog.Nop()
	cmd := NewInitCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", configPath, "--output", outputPath})
	err := cmd.Execute()
	require.NoError(t, err)

	// Verify gremlyn.yaml was created.
	_, statErr := os.Stat(outputPath)
	assert.NoError(t, statErr)

	output := buf.String()
	assert.Contains(t, output, "Generated")
}

func TestParseSingleMCPConfig(t *testing.T) {
	dir := t.TempDir()
	configJSON := `{"mcpServers":{"srv1":{"command":"node","args":["server.js"]},"srv2":{"url":"http://localhost:3000"}}}`
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(configJSON), 0o644))

	cfg, err := parseSingleMCPConfig(path)
	require.NoError(t, err)
	assert.Equal(t, "Custom", cfg.ClientName)
	assert.Len(t, cfg.Servers, 2)
	assert.Equal(t, "node", cfg.Servers["srv1"].Command)
	assert.Equal(t, "http://localhost:3000", cfg.Servers["srv2"].URL)
}
