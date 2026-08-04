package proxy

import (
	"context"
	"net/http"
	"strings"

	"github.com/rs/zerolog"
)

// CloudProxy extends HTTPProxy for cloud-hosted MCP servers.
// It injects authentication headers and enforces HTTPS.
type CloudProxy struct {
	*HTTPProxy
	authHeader string
	headers    map[string]string
}

// NewCloudProxy creates a CloudProxy that wraps HTTPProxy with auth headers.
func NewCloudProxy(cfg Config, opts ...Option) *CloudProxy {
	hp := NewHTTPProxy(cfg, opts...)
	hp.logger = hp.logger.With().Str("component", "cloud-proxy").Logger()

	return &CloudProxy{
		HTTPProxy:  hp,
		authHeader: cfg.AuthHeader,
		headers:    cfg.Headers,
	}
}

// Start overrides HTTPProxy.Start to inject a custom HTTP transport
// that adds auth headers to every upstream request.
func (c *CloudProxy) Start(ctx context.Context) error {
	// Wrap the HTTP client transport to inject headers on every request.
	base := http.DefaultTransport
	if c.client.Transport != nil {
		base = c.client.Transport
	}
	c.client.Transport = &authTransport{
		base:       base,
		authHeader: c.authHeader,
		headers:    c.headers,
		logger:     c.logger,
	}

	if c.upstream != nil && c.upstream.Scheme == "http" {
		c.logger.Warn().Str("upstream", c.upstream.String()).Msg("cloud mode using HTTP instead of HTTPS — consider using HTTPS in production")
	}

	c.logger.Info().
		Str("upstream", c.cfg.UpstreamURL).
		Bool("has_auth", c.authHeader != "").
		Int("custom_headers", len(c.headers)).
		Msg("cloud proxy starting")

	return c.HTTPProxy.Start(ctx)
}

// authTransport wraps an http.RoundTripper to inject auth and custom headers.
type authTransport struct {
	base       http.RoundTripper
	authHeader string
	headers    map[string]string
	logger     zerolog.Logger
}

// RoundTrip implements http.RoundTripper. It sends a clone of req with the
// configured auth header and custom headers applied, leaving the caller's
// request untouched. A raw (non-Bearer, non-Basic) auth value is sent as a
// Bearer token. It is safe for concurrent use as long as the configured headers
// are not mutated after construction.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid mutating the original.
	clone := req.Clone(req.Context())

	if t.authHeader != "" {
		if strings.HasPrefix(t.authHeader, "Bearer ") || strings.HasPrefix(t.authHeader, "Basic ") {
			clone.Header.Set("Authorization", t.authHeader)
		} else {
			// Assume it's a raw API key — wrap as Bearer token.
			clone.Header.Set("Authorization", "Bearer "+t.authHeader)
		}
	}

	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}

	return t.base.RoundTrip(clone)
}
