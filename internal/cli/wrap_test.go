package cli

import (
	"bytes"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestWrapCmd_NoArgs(t *testing.T) {
	logger := zerolog.Nop()
	cmd := NewWrapCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// No -- separator or command.
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestWrapCmd_MissingCommand(t *testing.T) {
	logger := zerolog.Nop()
	cmd := NewWrapCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// -- separator but no command after it.
	cmd.SetArgs([]string{"--"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestInferServerName_Match(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]config.ServerConfig{
			"hubspot": {Command: "npx"},
			"memory":  {Command: "node"},
		},
	}
	assert.Equal(t, "hubspot", inferServerName("npx", cfg))
	assert.Equal(t, "memory", inferServerName("node", cfg))
}

func TestInferServerName_Fallback(t *testing.T) {
	cfg := &config.Config{
		Servers: map[string]config.ServerConfig{
			"hubspot": {Command: "npx"},
		},
	}
	// Unknown command falls back to the command name itself.
	assert.Equal(t, "python", inferServerName("python", cfg))
}
