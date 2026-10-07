package proxy

import (
	"io"
	"os"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

type Config struct {
	ServerName string            `json:"server_name" yaml:"name"`
	Command    string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args       []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type Option func(*proxyOptions)

type proxyOptions struct {
	logger     zerolog.Logger
	pipeline   *Pipeline
	clientIn   io.Reader
	clientOut  io.Writer
	maxPayload int64
}

func WithLogger(logger zerolog.Logger) Option {
	return func(o *proxyOptions) {
		o.logger = logger
	}
}

func WithPipeline(pipeline *Pipeline) Option {
	return func(o *proxyOptions) {
		o.pipeline = pipeline
	}
}

func WithMaxPayload(limit int64) Option {
	return func(o *proxyOptions) {
		if limit > 0 {
			o.maxPayload = limit
		}
	}
}

func WithClientIO(in io.Reader, out io.Writer) Option {
	return func(o *proxyOptions) {
		o.clientIn = in
		o.clientOut = out
	}
}

func applyOptions(opts []Option) *proxyOptions {
	o := &proxyOptions{
		logger:     zerolog.Nop(),
		clientIn:   os.Stdin,
		clientOut:  os.Stdout,
		maxPayload: protocol.MaxLineBytes,
	}
	for _, opt := range opts {
		opt(o)
	}
	if o.pipeline == nil {
		o.pipeline = NewPipeline(o.logger)
	}
	return o
}
