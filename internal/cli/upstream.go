package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
)

type envLookup func(string) (string, bool)

func resolveHeaders(raw map[string]string, lookup envLookup) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make(map[string]string, len(raw))
	for _, name := range names {
		if !validHeaderName(name) {
			return nil, fmt.Errorf("header %q is not a valid HTTP header name", name)
		}
		if proxy.ReservedUpstreamHeader(name) {
			return nil, fmt.Errorf("header %q is managed by the proxy and cannot be set", name)
		}
		value, err := expandHeaderValue(name, raw[name], lookup)
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

func expandHeaderValue(name, value string, lookup envLookup) (string, error) {
	var b strings.Builder
	rest := value
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		end := strings.IndexByte(rest[start+2:], '}')
		if end < 0 {
			return "", fmt.Errorf("header %q: an unterminated ${ in its value", name)
		}
		ref := rest[start+2 : start+2+end]
		if !validEnvName(ref) {
			return "", fmt.Errorf("header %q: an invalid variable reference in its value, expected ${NAME}", name)
		}
		v, ok := lookup(ref)
		if !ok || v == "" {
			return "", fmt.Errorf("header %q: environment variable %s is not set", name, ref)
		}
		b.WriteString(v)
		rest = rest[start+2+end+1:]
	}

	out := b.String()
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("header %q has an empty value", name)
	}
	if strings.ContainsAny(out, "\r\n\x00") {
		return "", fmt.Errorf("header %q: its value contains a line break or a NUL byte", name)
	}
	return out, nil
}

func validEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func validHeaderName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

func parseHeaderFlags(flags []string) (map[string]string, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(flags))
	for _, f := range flags {
		name, value, ok := strings.Cut(f, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, errors.New(`--upstream-header must be "Name: value"`)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("--upstream-header %q is given more than once", name)
		}
		out[name] = strings.TrimSpace(value)
	}
	return out, nil
}

func validateMCPServer(srv *MCPServerConfig, lookup envLookup) error {
	hasCommand, hasURL := len(srv.Command) > 0, srv.URL != ""
	switch {
	case hasCommand && hasURL:
		return errors.New("mcp_server takes command (stdio) or url (Streamable HTTP), not both")
	case !hasCommand && !hasURL:
		return errors.New("mcp_server needs command (stdio) or url (Streamable HTTP)")
	case hasCommand:
		if len(srv.Headers) > 0 {
			return errors.New("mcp_server.headers applies to a url server only")
		}
		if srv.RequestTimeout != 0 {
			return errors.New("mcp_server.request_timeout applies to a url server only")
		}
		return nil
	}

	if _, err := proxy.ValidateUpstreamURL(srv.URL); err != nil {
		return fmt.Errorf("mcp_server.%w", err)
	}
	if srv.RequestTimeout < 0 {
		return fmt.Errorf("mcp_server.request_timeout must be positive, got %s", srv.RequestTimeout)
	}
	if _, err := resolveHeaders(srv.Headers, lookup); err != nil {
		return fmt.Errorf("mcp_server.headers: %w", err)
	}
	return nil
}
