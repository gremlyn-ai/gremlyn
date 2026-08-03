package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionCmd(t *testing.T) {
	cmd := NewVersionCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "gremlyn version")
	assert.Contains(t, output, "dev")
}

func TestVersionCmd_CustomValues(t *testing.T) {
	origVersion, origCommit, origDate := Version, Commit, BuildDate
	defer func() {
		Version, Commit, BuildDate = origVersion, origCommit, origDate
	}()

	Version = "1.0.0"
	Commit = "abc123"
	BuildDate = "2025-01-01"

	cmd := NewVersionCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "1.0.0")
	assert.Contains(t, output, "abc123")
	assert.Contains(t, output, "2025-01-01")
}
