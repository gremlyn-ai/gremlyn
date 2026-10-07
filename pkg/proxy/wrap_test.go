package proxy

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewWrapProxy(t *testing.T) {
	cfg := Config{
		ServerName: "test-server",
		Command:    "echo",
		Args:       []string{"hello"},
	}

	proxy := NewWrapProxy(cfg, WithLogger(zerolog.Nop()))
	assert.NotNil(t, proxy)
}

func TestWrapProxy_EnvMerge(t *testing.T) {
	cfg := Config{
		ServerName: "test",
		Command:    "echo",
		Env:        map[string]string{"MY_VAR": "my_value"},
	}

	proxy := NewWrapProxy(cfg)
	assert.Equal(t, "my_value", proxy.cfg.Env["MY_VAR"])
}
