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

const doctorTestYAML = `
version: 1
mode: shield

servers:
  test:
    mode: wrap
    rules:
      - name: "test rule"
        action: allow
`

func TestDoctorCmd_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gremlyn.yaml")
	require.NoError(t, os.WriteFile(path, []byte(doctorTestYAML), 0o644))

	logger := zerolog.Nop()
	cmd := NewDoctorCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", path})
	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Gremlyn Doctor")
	assert.Contains(t, output, "[OK] gremlyn.yaml")
}

func TestDoctorCmd_MissingConfig(t *testing.T) {
	logger := zerolog.Nop()
	cmd := NewDoctorCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", "/nonexistent/gremlyn.yaml"})
	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "[FAIL] gremlyn.yaml")
	assert.Contains(t, output, "Some checks failed")
}

func TestDoctorCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gremlyn.yaml")
	require.NoError(t, os.WriteFile(path, []byte(doctorTestYAML), 0o644))

	logger := zerolog.Nop()
	cmd := NewDoctorCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"--config", path, "--json"})
	err := cmd.Execute()
	require.NoError(t, err)

	var checks []checkResult
	err = json.Unmarshal(buf.Bytes(), &checks)
	require.NoError(t, err)
	require.NotEmpty(t, checks)

	assert.Equal(t, "gremlyn.yaml", checks[0].Name)
	assert.Equal(t, "ok", checks[0].Status)
}
