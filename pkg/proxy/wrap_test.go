package proxy

import (
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestNewWrapProxy(t *testing.T) {
	cfg := Config{
		ServerName: "test-server",
		Mode:       models.ServerModeWrap,
		Command:    "echo",
		Args:       []string{"hello"},
	}

	proxy := NewWrapProxy(cfg, WithLogger(zerolog.Nop()))
	assert.NotNil(t, proxy)
	assert.Equal(t, "test-server", proxy.ServerName())
	assert.NotNil(t, proxy.Pipeline())
}

func TestWrapProxy_Pipeline(t *testing.T) {
	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	cfg := Config{
		ServerName: "test",
		Mode:       models.ServerModeWrap,
		Command:    "echo",
	}

	proxy := NewWrapProxy(cfg, WithLogger(logger), WithPipeline(pipeline))
	assert.Equal(t, pipeline, proxy.Pipeline())
}

func TestWrapProxy_EnvMerge(t *testing.T) {
	cfg := Config{
		ServerName: "test",
		Mode:       models.ServerModeWrap,
		Command:    "echo",
		Env:        map[string]string{"MY_VAR": "my_value"},
	}

	proxy := NewWrapProxy(cfg)
	assert.Equal(t, "my_value", proxy.cfg.Env["MY_VAR"])
}
