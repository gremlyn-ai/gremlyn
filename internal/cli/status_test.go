package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const statusTestYAML = `
version: 1
mode: shield

servers:
  hubspot:
    mode: wrap
    command: npx
    rules:
      - name: "Block bulk exports"
        match:
          tool: "search_contacts"
        action: block
  postgres:
    mode: proxy
    upstream: "http://localhost:5433/mcp"
    rules:
      - name: "SELECT only"
        action: allow

global:
  rate_limit: 50/minute
  max_payload_size: 1MB
`

func TestStatusCmd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gremlyn.yaml")
	require.NoError(t, os.WriteFile(path, []byte(statusTestYAML), 0o644))

	logger := zerolog.Nop()
	cmd := NewStatusCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", path})
	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Gremlyn Status")
	assert.Contains(t, output, "shield")
	assert.Contains(t, output, "hubspot")
	assert.Contains(t, output, "postgres")
	assert.Contains(t, output, "Block bulk exports")
	assert.Contains(t, output, "SELECT only")
	assert.Contains(t, output, "50/minute")
	assert.Contains(t, output, "1MB")
}

func TestStatusCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gremlyn.yaml")
	require.NoError(t, os.WriteFile(path, []byte(statusTestYAML), 0o644))

	logger := zerolog.Nop()
	cmd := NewStatusCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", path, "--json"})
	err := cmd.Execute()
	require.NoError(t, err)

	var result config.Config
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Version)
	assert.Equal(t, "shield", result.Mode)
}

func TestStatusCmd_MissingConfig(t *testing.T) {
	logger := zerolog.Nop()
	cmd := NewStatusCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", "/nonexistent/gremlyn.yaml"})
	err := cmd.Execute()
	assert.Error(t, err)
}
