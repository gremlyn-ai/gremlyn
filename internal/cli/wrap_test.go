package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapCmd_NoArgs(t *testing.T) {
	logger := zerolog.Nop()
	cmd := NewWrapCmd(logger)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
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
	cmd.SetArgs([]string{"--"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestSessionEndedNormally(t *testing.T) {
	tests := []struct {
		name     string
		runErr   error
		ctxErr   error
		signaled bool
		want     bool
	}{
		{"stdin closed by the client", nil, nil, false, true},
		{"client sent SIGTERM", nil, context.Canceled, true, true},
		{"client sent SIGTERM, proxy reports cancel", context.Canceled, context.Canceled, true, true},
		{"proxy failed", errors.New("child exited"), nil, false, false},
		{"proxy failed during a signal", errors.New("broken pipe"), context.Canceled, true, false},
		{"cancelled without a signal", nil, context.Canceled, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sessionEndedNormally(tt.runErr, tt.ctxErr, tt.signaled))
		})
	}
}

func TestWrapCmd_ChaosParamsAreStrict(t *testing.T) {
	tests := []struct{ name, params, wantErr string }{
		{"unknown field", `{"injection_txt":"x"}`, `unknown field "injection_txt"`},
		{"param no gremlin reads", `{"payload_bytes":64}`, "payload_bytes set but no gremlin of this scenario reads it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewWrapCmd(zerolog.Nop())
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs([]string{"--chaos-gremlins", "injection", "--chaos-params", tt.params, "--", "true"})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
