package proxy

import (
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProxy_WrapMode(t *testing.T) {
	cfg := Config{
		ServerName: "test",
		Mode:       models.ServerModeWrap,
		Command:    "npx",
		Args:       []string{"@hubspot/mcp-server"},
	}
	proxy, err := NewProxy(cfg)
	require.NoError(t, err)
	assert.NotNil(t, proxy)
	assert.Equal(t, "test", proxy.ServerName())
	assert.IsType(t, &WrapProxy{}, proxy)
}

func TestNewProxy_ProxyMode(t *testing.T) {
	cfg := Config{
		ServerName:  "postgres",
		Mode:        models.ServerModeProxy,
		UpstreamURL: "http://localhost:5433/mcp",
	}
	proxy, err := NewProxy(cfg)
	require.NoError(t, err)
	assert.NotNil(t, proxy)
	assert.IsType(t, &HTTPProxy{}, proxy)
}

func TestNewProxy_ProxyMode_DefaultListenAddr(t *testing.T) {
	cfg := Config{
		ServerName:  "test",
		Mode:        models.ServerModeProxy,
		UpstreamURL: "http://localhost:5433/mcp",
	}
	_, err := NewProxy(cfg)
	require.NoError(t, err)
}

func TestNewProxy_WrapMode_MissingCommand(t *testing.T) {
	cfg := Config{
		ServerName: "test",
		Mode:       models.ServerModeWrap,
	}
	_, err := NewProxy(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command")
}

func TestNewProxy_ProxyMode_MissingURL(t *testing.T) {
	cfg := Config{
		ServerName: "test",
		Mode:       models.ServerModeProxy,
	}
	_, err := NewProxy(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upstream URL")
}

func TestNewProxy_UnknownMode(t *testing.T) {
	cfg := Config{
		ServerName: "test",
		Mode:       "unknown",
	}
	_, err := NewProxy(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestNewProxy_WithOptions(t *testing.T) {
	cfg := Config{
		ServerName: "test",
		Mode:       models.ServerModeWrap,
		Command:    "echo",
	}
	proxy, err := NewProxy(cfg, WithTimeout(60_000_000_000))
	require.NoError(t, err)
	assert.NotNil(t, proxy)
	assert.NotNil(t, proxy.Pipeline())
}
