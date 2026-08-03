package main

import (
	"bytes"
	"testing"

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
	assert.Contains(t, output, "init")
	assert.Contains(t, output, "wrap")
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "doctor")
	assert.Contains(t, output, "version")
	assert.Contains(t, output, "shield")
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

func TestRootCmd_ShieldSubcommands(t *testing.T) {
	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"shield", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "rules")
	assert.Contains(t, output, "logs")
}

func TestRootCmd_ArenaSubcommands(t *testing.T) {
	cmd := newRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"arena", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "status")
	assert.Contains(t, output, "sessions")
	assert.Contains(t, output, "list-gremlins")
}

func TestNewLogger(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error", "invalid"} {
		logger := newLogger(level)
		assert.NotNil(t, logger)
	}
}
