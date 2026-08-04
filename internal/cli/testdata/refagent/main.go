// Command refagent is a minimal, scripted MCP agent used to test Arena itself.
//
// It exists because a real LLM agent is the wrong instrument for validating a
// measurement: it is nondeterministic, costs money per run, and its behaviour
// changes under you. To check that the resilience score actually discriminates
// between a robust and a fragile agent, the two behaviours have to be fixed and
// known — which is exactly what this program provides.
//
//	--mode=robust    notices a bad tool result and retries the same tool
//	--mode=fragile   accepts whatever comes back and stops
//
// It speaks the same wire format as any MCP client: newline-delimited JSON-RPC
// over the child's stdio.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type mcpFile struct {
	MCPServers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"mcpServers"`
}

type rpc struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func main() {
	mode := flag.String("mode", "robust", "robust or fragile")
	cfgPath := flag.String("mcp-config", "", "path to the MCP client config")
	tool := flag.String("tool", "read_graph", "tool to call")
	retries := flag.Int("retries", 1, "how many times a robust agent retries")
	expect := flag.String("expect-key", "ok",
		"a top-level key the result must contain; a robust agent retries when it is missing")
	flag.String("prompt", "", "ignored; accepted so the CI config can pass one")
	flag.Parse()

	if err := run(*mode, *cfgPath, *tool, *expect, *retries); err != nil {
		fmt.Fprintf(os.Stderr, "refagent: %v\n", err)
		os.Exit(1)
	}
}

func run(mode, cfgPath, tool, expectKey string, retries int) error {
	if cfgPath == "" {
		return fmt.Errorf("--mcp-config is required")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("reading MCP config: %w", err)
	}
	var cfg mcpFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing MCP config: %w", err)
	}
	if len(cfg.MCPServers) == 0 {
		return fmt.Errorf("no mcpServers in %s", cfgPath)
	}

	// Deterministic pick: the config the harness writes has exactly one server.
	var command string
	var args []string
	for _, s := range cfg.MCPServers {
		command, args = s.Command, s.Args
		break
	}

	cmd := exec.Command(command, args...) //nolint:gosec // the command comes from our own generated config
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting MCP server: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	r := bufio.NewReaderSize(stdout, 1<<20)
	id := 0
	next := func() *int { id++; v := id; return &v }

	send := func(m rpc) error {
		m.JSONRPC = "2.0"
		b, err := json.Marshal(m)
		if err != nil {
			return fmt.Errorf("marshalling %s: %w", m.Method, err)
		}
		if _, err := stdin.Write(append(b, '\n')); err != nil {
			return fmt.Errorf("writing %s: %w", m.Method, err)
		}
		return nil
	}

	read := func() (*rpc, error) {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				if err == io.EOF && strings.TrimSpace(line) == "" {
					return nil, io.EOF
				}
				if err != io.EOF {
					return nil, fmt.Errorf("reading: %w", err)
				}
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var m rpc
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				// Corrupted framing is itself something a gremlin may cause; skip
				// the line rather than dying, which is the robust behaviour.
				continue
			}
			if m.ID == nil && m.Method != "" {
				continue // a notification from the server
			}
			return &m, nil
		}
	}

	// Handshake.
	if err := send(rpc{ID: next(), Method: "initialize", Params: map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "refagent", "version": "1"},
	}}); err != nil {
		return err
	}
	if _, err := read(); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	if err := send(rpc{Method: "notifications/initialized"}); err != nil {
		return err
	}
	if err := send(rpc{ID: next(), Method: "tools/list"}); err != nil {
		return err
	}
	if _, err := read(); err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}

	// Call the tool, then react according to the configured behaviour.
	attempts := 1
	if mode == "robust" {
		attempts += retries
	}

	for attempt := range attempts {
		if err := send(rpc{ID: next(), Method: "tools/call", Params: map[string]any{
			"name":      tool,
			"arguments": map[string]any{},
		}}); err != nil {
			return err
		}

		resp, err := read()
		if err != nil {
			// No answer at all. A robust agent retries; a fragile one gives up.
			if mode == "robust" && attempt < attempts-1 {
				continue
			}
			return nil
		}

		if !suspicious(resp, expectKey) {
			return nil // the result looked fine: nothing to react to
		}
		if mode != "robust" {
			// The fragile agent accepts the bad result and stops. This is the
			// behaviour Arena is meant to catch.
			return nil
		}
	}
	return nil
}

// suspicious reports whether a tool result looks wrong enough to warrant a retry.
//
// The rule is fixed and explainable, not clever: an error, an empty or
// unparseable payload, or a payload missing a key the caller knows it asked for.
//
// That last check is the one that matters. A field-dropping gremlin leaves
// perfectly valid JSON behind, so an agent that only asks "does this parse?"
// cannot tell a corrupted result from a good one — and then a robust agent and a
// fragile one behave identically, which is exactly what the discriminance gate
// caught the first time it ran.
func suspicious(m *rpc, expectKey string) bool {
	if m.Error != nil {
		return true
	}
	if len(m.Result) == 0 {
		return true
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(m.Result, &obj); err != nil {
		return true
	}
	if expectKey == "" {
		return false
	}
	_, ok := obj[expectKey]
	return !ok
}
