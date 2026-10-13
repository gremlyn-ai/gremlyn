package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
)

type envLookup func(string) (string, bool)

type resolvedUpstream struct {
	URL     string
	Headers map[string]string
	Vars    []string
}

func resolveUpstream(srv *MCPServerConfig, lookup envLookup) (resolvedUpstream, error) {
	used := map[string]bool{}
	u, err := resolveURL(srv.URL, lookup, used)
	if err != nil {
		return resolvedUpstream{}, fmt.Errorf("mcp_server.%w", err)
	}
	headers, err := resolveHeaders(srv.Headers, lookup, used)
	if err != nil {
		return resolvedUpstream{}, fmt.Errorf("mcp_server.headers: %w", err)
	}
	vars := make([]string, 0, len(used))
	for name := range used {
		vars = append(vars, name)
	}
	sort.Strings(vars)
	return resolvedUpstream{URL: u, Headers: headers, Vars: vars}, nil
}

func scrubEnv(environ, vars []string) []string {
	if len(vars) == 0 {
		return environ
	}
	drop := make(map[string]bool, len(vars))
	for _, v := range vars {
		drop[v] = true
	}
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			out = append(out, kv)
		}
	}
	return out
}

func resolveHeaders(raw map[string]string, lookup envLookup, used map[string]bool) (map[string]string, error) {
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
		value, err := expandHeaderValue(name, raw[name], lookup, used)
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

func expandHeaderValue(name, value string, lookup envLookup, used map[string]bool) (string, error) {
	what := fmt.Sprintf("header %q", name)
	out, err := expandEnvRefs(what, value, lookup, used)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("%s has an empty value", what)
	}
	return out, nil
}

func resolveURL(raw string, lookup envLookup, used map[string]bool) (string, error) {
	return expandEnvRefs("url", raw, lookup, used)
}

func expandEnvRefs(what, value string, lookup envLookup, used map[string]bool) (string, error) {
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
			return "", fmt.Errorf("%s: an unterminated ${ in its value", what)
		}
		ref := rest[start+2 : start+2+end]
		if !validEnvName(ref) {
			return "", fmt.Errorf("%s: an invalid variable reference in its value, expected ${NAME}", what)
		}
		if ciCredentialVar(ref) {
			return "", fmt.Errorf("%s: ${%s} is a CI credential variable, which gremlyn never sends to a server", what, ref)
		}
		v, ok := lookup(ref)
		if !ok || v == "" {
			return "", fmt.Errorf("%s: environment variable %s is not set", what, ref)
		}
		b.WriteString(v)
		if used != nil {
			used[ref] = true
		}
		rest = rest[start+2+end+1:]
	}

	out := b.String()
	if strings.ContainsAny(out, "\r\n\x00") {
		return "", fmt.Errorf("%s: its value contains a line break or a NUL byte", what)
	}
	return out, nil
}

func ciCredentialVar(name string) bool {
	return name == "GH_TOKEN" || strings.HasPrefix(name, "GITHUB_") || strings.HasPrefix(name, "ACTIONS_")
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

	resolved, err := resolveUpstream(srv, lookup)
	if err != nil {
		return err
	}
	if _, err := proxy.ValidateUpstreamURL(resolved.URL); err != nil {
		return fmt.Errorf("mcp_server.%w", err)
	}
	if srv.RequestTimeout < 0 {
		return fmt.Errorf("mcp_server.request_timeout must be positive, got %s", srv.RequestTimeout)
	}
	return nil
}

func checkAgentEnv(agentEnv map[string]string, vars []string) error {
	for _, v := range vars {
		if _, ok := agentEnv[v]; ok {
			return fmt.Errorf("agent.env sets %s, which mcp_server reads for its url or headers: "+
				"the agent never sees those variables", v)
		}
	}
	return nil
}
