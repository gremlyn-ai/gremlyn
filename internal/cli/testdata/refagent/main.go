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

	var command string
	var args []string
	for _, s := range cfg.MCPServers {
		command, args = s.Command, s.Args
		break
	}

	cmd := exec.Command(command, args...)
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
				continue
			}
			if m.ID == nil && m.Method != "" {
				continue
			}
			return &m, nil
		}
	}

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
			if mode == "robust" && attempt < attempts-1 {
				continue
			}
			return nil
		}

		if !suspicious(resp, expectKey) {
			return nil
		}
		if mode != "robust" {
			return nil
		}
	}
	return nil
}

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
