package main

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRootCmd_Help(t *testing.T) {
	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	err := cmd.Execute()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "gremlyn")
	assert.Contains(t, output, "wrap")
	assert.Contains(t, output, "version")
	assert.Contains(t, output, "arena")
}

func TestRootCmd_Version(t *testing.T) {
	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"version"})
	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "gremlyn version")
}

func TestRootCmd_ArenaSubcommands(t *testing.T) {
	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"arena", "--help"})
	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "ci")
	assert.Contains(t, output, "replay")
	assert.Contains(t, output, "sessions")
	assert.Contains(t, output, "list-gremlins")
}

func TestNewLogger(t *testing.T) {
	assert.NotNil(t, newLogger())
}

func TestLogLevelFlagAppliesGlobally(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-level", "debug", "version"})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, zerolog.DebugLevel, zerolog.GlobalLevel())

	cmd = newRootCmd()
	cmd.SetArgs([]string{"--log-level", "error", "version"})
	require.NoError(t, cmd.Execute())
	assert.Equal(t, zerolog.ErrorLevel, zerolog.GlobalLevel())
}

func TestLogLevelFlagRejectsGarbage(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--log-level", "verbose", "version"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --log-level")
}
