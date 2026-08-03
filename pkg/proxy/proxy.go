package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

// Proxy is the interface that consumers (Shield, Arena, CLI) use to create
// and control proxy instances. It abstracts the underlying transport mode.
type Proxy interface {
	// Start starts the proxy and blocks until ctx is cancelled or Stop is called.
	Start(ctx context.Context) error
	// Stop initiates graceful shutdown.
	Stop() error
	// Pipeline returns the analysis pipeline so handlers can be registered.
	Pipeline() *Pipeline
	// ServerName returns the name of the MCP server this proxy fronts.
	ServerName() string
}

// Config holds the configuration for creating a proxy instance.
type Config struct {
	ServerName  string            `json:"server_name" yaml:"name"`
	Mode        models.ServerMode `json:"mode" yaml:"mode"`
	Command     string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args        []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	UpstreamURL string            `json:"upstream_url,omitempty" yaml:"upstream_url,omitempty"`
	ListenAddr  string            `json:"listen_addr,omitempty" yaml:"listen_addr,omitempty"`
	AuthHeader  string            `json:"auth_header,omitempty" yaml:"auth_header,omitempty"` // Cloud mode: Authorization header value
	Headers     map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`         // Cloud mode: additional headers
}

// Option is a functional option for configuring proxy instances.
type Option func(*proxyOptions)

type proxyOptions struct {
	logger   zerolog.Logger
	pipeline *Pipeline
	timeout  time.Duration
}

// WithLogger sets the logger for the proxy.
func WithLogger(logger zerolog.Logger) Option {
	return func(o *proxyOptions) {
		o.logger = logger
	}
}

// WithPipeline injects a pre-configured pipeline into the proxy.
func WithPipeline(pipeline *Pipeline) Option {
	return func(o *proxyOptions) {
		o.pipeline = pipeline
	}
}

// WithTimeout sets the overall proxy timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *proxyOptions) {
		o.timeout = d
	}
}

func applyOptions(opts []Option) *proxyOptions {
	o := &proxyOptions{
		logger:  zerolog.Nop(),
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(o)
	}
	if o.pipeline == nil {
		o.pipeline = NewPipeline(o.logger)
	}
	return o
}

// NewProxy creates a proxy instance based on the config mode.
// Returns a WrapProxy for wrap mode and an HTTPProxy for proxy mode.
func NewProxy(cfg Config, opts ...Option) (Proxy, error) {
	switch cfg.Mode {
	case models.ServerModeWrap:
		if cfg.Command == "" {
			return nil, fmt.Errorf("wrap mode requires a command")
		}
		return NewWrapProxy(cfg, opts...), nil

	case models.ServerModeProxy:
		if cfg.UpstreamURL == "" {
			return nil, fmt.Errorf("proxy mode requires an upstream URL")
		}
		if cfg.ListenAddr == "" {
			cfg.ListenAddr = ":9090"
		}
		return NewHTTPProxy(cfg, opts...), nil

	case models.ServerModeCloud:
		if cfg.UpstreamURL == "" {
			return nil, fmt.Errorf("cloud mode requires an upstream URL")
		}
		if cfg.ListenAddr == "" {
			cfg.ListenAddr = ":9090"
		}
		return NewCloudProxy(cfg, opts...), nil

	default:
		return nil, fmt.Errorf("unsupported proxy mode: %q", cfg.Mode)
	}
}
