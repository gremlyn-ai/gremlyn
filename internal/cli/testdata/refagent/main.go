package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type mcpServer struct {
	Type    string   `json:"type"`
	URL     string   `json:"url"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type mcpFile struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
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
	dumpEnv := flag.String("dump-env", "", "write the agent's environment to this file")
	flag.Parse()

	if *dumpEnv != "" {
		if err := os.WriteFile(*dumpEnv, []byte(strings.Join(os.Environ(), "\n")), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "refagent: %v\n", err)
			os.Exit(1)
		}
	}

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

	var server mcpServer
	for _, srv := range cfg.MCPServers {
		server = srv
		break
	}

	var conn transport
	if server.Type == "http" {
		conn = newHTTPTransport(server.URL)
	} else {
		conn, err = newStdioTransport(server.Command, server.Args)
		if err != nil {
			return err
		}
	}
	defer conn.close()

	id := 0
	next := func() *int { id++; v := id; return &v }
	send := conn.send
	read := conn.read

	protocolVersion := "2024-11-05"
	if server.Type == "http" {
		protocolVersion = "2025-06-18"
	}
	if err := send(rpc{ID: next(), Method: "initialize", Params: map[string]any{
		"protocolVersion": protocolVersion,
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

type transport interface {
	send(m rpc) error
	read() (*rpc, error)
	close()
}

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
}

func newStdioTransport(command string, args []string) (*stdioTransport, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting MCP server: %w", err)
	}
	return &stdioTransport{cmd: cmd, stdin: stdin, reader: bufio.NewReaderSize(stdout, 1<<20)}, nil
}

func (s *stdioTransport) send(m rpc) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", m.Method, err)
	}
	if _, err := s.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("writing %s: %w", m.Method, err)
	}
	return nil
}

func (s *stdioTransport) read() (*rpc, error) {
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && strings.TrimSpace(line) == "" {
				return nil, io.EOF
			}
			if !errors.Is(err, io.EOF) {
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

func (s *stdioTransport) close() {
	_ = s.stdin.Close()
	_ = s.cmd.Wait()
}

type httpTransport struct {
	url             string
	client          *http.Client
	session         string
	protocolVersion string
	queue           []*rpc
}

func newHTTPTransport(url string) *httpTransport {
	return &httpTransport{url: url, client: &http.Client{Timeout: 60 * time.Second}}
}

func (h *httpTransport) do(method string, body []byte) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	req, err := http.NewRequestWithContext(ctx, method, h.url, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("building %s: %w", method, err)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if h.session != "" {
		req.Header.Set("Mcp-Session-Id", h.session)
	}
	if h.protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", h.protocolVersion)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	resp.Body = cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func (h *httpTransport) send(m rpc) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", m.Method, err)
	}
	resp, err := h.do(http.MethodPost, b)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		h.session = sid
	}
	if resp.StatusCode == http.StatusAccepted {
		return nil
	}

	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mt == "text/event-stream" {
		return h.readStream(resp.Body)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading %s: %w", m.Method, err)
	}
	var out rpc
	if err := json.Unmarshal(data, &out); err != nil || out.ID == nil {
		return fmt.Errorf("%s: HTTP %d", m.Method, resp.StatusCode)
	}
	h.enqueue(&out)
	return nil
}

func (h *httpTransport) readStream(body io.Reader) error {
	r := bufio.NewReader(body)
	var data []string
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case line == "" && len(data) > 0:
			var m rpc
			if json.Unmarshal([]byte(strings.Join(data, "\n")), &m) == nil && m.ID != nil {
				h.enqueue(&m)
			}
			data = nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading SSE: %w", err)
		}
	}
}

func (h *httpTransport) enqueue(m *rpc) {
	if h.protocolVersion == "" && len(m.Result) > 0 {
		var init struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(m.Result, &init) == nil && init.ProtocolVersion != "" {
			h.protocolVersion = init.ProtocolVersion
		}
	}
	h.queue = append(h.queue, m)
}

func (h *httpTransport) read() (*rpc, error) {
	if len(h.queue) == 0 {
		return nil, io.EOF
	}
	m := h.queue[0]
	h.queue = h.queue[1:]
	return m, nil
}

func (h *httpTransport) close() {
	if h.session == "" {
		return
	}
	resp, err := h.do(http.MethodDelete, nil)
	if err == nil {
		_ = resp.Body.Close()
	}
}
