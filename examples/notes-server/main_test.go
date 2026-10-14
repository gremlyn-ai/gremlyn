package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type client struct {
	t       *testing.T
	url     string
	session string
	next    int
}

func newClient(t *testing.T) (*client, *server) {
	t.Helper()
	s := newServer(zerolog.Nop())
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	c := &client{t: t, url: ts.URL + "/mcp"}
	resp := c.post("initialize", map[string]any{"protocolVersion": "2025-06-18"})
	require.Nil(t, resp.Error)
	require.NotEmpty(t, c.session, "initialize starts a session")
	return c, s
}

type reply struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *client) raw(method string, params any, session string) *http.Response {
	c.t.Helper()
	c.next++
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	require.NoError(c.t, err)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.url, bytes.NewReader(body))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(c.t, err)
	return resp
}

func (c *client) post(method string, params any) reply {
	c.t.Helper()
	resp := c.raw(method, params, c.session)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(c.t, http.StatusOK, resp.StatusCode)
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.session = sid
	}
	var r reply
	require.NoError(c.t, json.NewDecoder(resp.Body).Decode(&r))
	return r
}

func (c *client) callText(name string, args map[string]any) string {
	c.t.Helper()
	r := c.post("tools/call", map[string]any{"name": name, "arguments": args})
	require.Nil(c.t, r.Error)
	var res toolResult
	require.NoError(c.t, json.Unmarshal(r.Result, &res))
	require.Len(c.t, res.Content, 1)
	return res.Content[0].Text
}

func TestToolsAreAnnotated(t *testing.T) {
	c, _ := newClient(t)
	r := c.post("tools/list", nil)
	var list struct {
		Tools []struct {
			Name        string                     `json:"name"`
			Annotations map[string]json.RawMessage `json:"annotations"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(r.Result, &list))

	want := map[string]map[string]string{
		"list_notes":  {"readOnlyHint": "true", "openWorldHint": "false"},
		"create_note": {"readOnlyHint": "false", "destructiveHint": "false", "idempotentHint": "false", "openWorldHint": "false"},
		"delete_note": {"readOnlyHint": "false", "destructiveHint": "true", "idempotentHint": "true", "openWorldHint": "false"},
	}
	require.Len(t, list.Tools, len(want))
	for _, tool := range list.Tools {
		for hint, value := range want[tool.Name] {
			assert.Equal(t, value, string(tool.Annotations[hint]), "%s %s", tool.Name, hint)
		}
	}
}

func TestReadCreateDelete(t *testing.T) {
	c, s := newClient(t)

	listing := c.callText("list_notes", nil)
	assert.Contains(t, listing, "#3 scratch: ", "the read tool returns free text")

	assert.Equal(t, `Created note #4, "groceries".`, c.callText("create_note", map[string]any{"title": "groceries", "body": "bread"}))
	assert.Equal(t, `Created note #5, "groceries".`, c.callText("create_note", map[string]any{"title": "groceries", "body": "bread"}),
		"create is not idempotent: the same call twice makes two notes")

	assert.Equal(t, "Deleted note #3.", c.callText("delete_note", map[string]any{"id": 3}))
	assert.Equal(t, "There is no note #3.", c.callText("delete_note", map[string]any{"id": 3}),
		"delete is idempotent: the second call changes nothing")
	s.mu.Lock()
	assert.Len(t, s.notes, 4)
	s.mu.Unlock()

	r := c.post("tools/call", map[string]any{"name": "create_note", "arguments": map[string]any{"title": " "}})
	var res toolResult
	require.NoError(t, json.Unmarshal(r.Result, &res))
	assert.True(t, res.IsError)

	r = c.post("tools/call", map[string]any{"name": "drop_table"})
	require.NotNil(t, r.Error)
	assert.Equal(t, -32602, r.Error.Code)
	r = c.post("resources/list", nil)
	require.NotNil(t, r.Error)
	assert.Equal(t, -32601, r.Error.Code)
}

func TestTransport(t *testing.T) {
	c, _ := newClient(t)

	resp := c.raw("tools/list", nil, "")
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "a request outside a session is refused")

	resp = c.raw("tools/list", nil, "not-a-session")
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	for _, tt := range []struct {
		method, path, origin, body string
		want                       int
	}{
		{http.MethodGet, "/mcp", "", "", http.StatusMethodNotAllowed},
		{http.MethodPost, "/other", "", "{}", http.StatusNotFound},
		{http.MethodPost, "/mcp", "https://evil.example", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, http.StatusForbidden},
		{http.MethodPost, "/mcp", "", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, http.StatusBadRequest},
		{http.MethodPost, "/mcp", "http://localhost:3000", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, http.StatusAccepted},
		{http.MethodDelete, "/mcp", "", "", http.StatusOK},
	} {
		req, err := http.NewRequestWithContext(context.Background(), tt.method, strings.TrimSuffix(c.url, "/mcp")+tt.path, strings.NewReader(tt.body))
		require.NoError(t, err)
		req.Header.Set("Mcp-Session-Id", c.session)
		if tt.origin != "" {
			req.Header.Set("Origin", tt.origin)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		assert.Equal(t, tt.want, resp.StatusCode, "%s %s origin=%q", tt.method, tt.path, tt.origin)
	}

	resp = c.raw("tools/list", nil, c.session)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "DELETE ends the session")
}

func TestNegotiatesTheProtocolVersion(t *testing.T) {
	for _, tt := range []struct{ asked, want string }{
		{"2025-06-18", "2025-06-18"},
		{"2025-03-26", "2025-03-26"},
		{"1999-01-01", latestProtocol},
	} {
		got := initializeResult(json.RawMessage(`{"protocolVersion":"` + tt.asked + `"}`))
		assert.Equal(t, tt.want, got.ProtocolVersion)
	}
}

func TestListensOnLoopbackOnly(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:0", "localhost:0", "[::1]:0"} {
		assert.NoError(t, checkLoopback(addr), addr)
	}
	for _, addr := range []string{"0.0.0.0:8931", ":8931", "192.168.1.10:8931", "example.com:80", "nonsense"} {
		assert.Error(t, checkLoopback(addr), addr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.NoError(t, run(ctx, "127.0.0.1:0", zerolog.Nop()), "a cancelled context shuts the server down cleanly")
	assert.Error(t, run(context.Background(), "0.0.0.0:0", zerolog.Nop()))
}

func TestDefinitionsAreFreshCopies(t *testing.T) {
	tl := toolList()
	tl[0].Name = "mutated"
	assert.Equal(t, "list_notes", toolList()[0].Name)

	pv := supportedProtocols()
	pv[0] = "mutated"
	assert.Equal(t, latestProtocol, supportedProtocols()[0])
}
