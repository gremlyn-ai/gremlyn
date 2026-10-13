package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const e2eTokenEnv = "GREMLYN_E2E_TOKEN"

type fakeHTTPMode int

const (
	modeJSON fakeHTTPMode = iota
	modeSSE
	modeHang
	modeLegacy
)

type fakeHTTPMCP struct {
	*httptest.Server
	mode    fakeHTTPMode
	mu      sync.Mutex
	auth    []string
	deletes int
}

func newFakeHTTPMCP(t *testing.T, mode fakeHTTPMode) *fakeHTTPMCP {
	t.Helper()
	f := &fakeHTTPMCP{mode: mode}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeHTTPMCP) URL() string { return f.Server.URL + "/mcp" }

func (f *fakeHTTPMCP) authHeaders() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auth...)
}

func (f *fakeHTTPMCP) deleteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deletes
}

func (f *fakeHTTPMCP) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.mu.Unlock()

	if f.mode == modeLegacy {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "event: endpoint\ndata: /messages?sessionId=1\n\n")
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if want := os.Getenv(e2eTokenEnv); want == "" || r.Header.Get("Authorization") != "Bearer "+want {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://fake.example/.well-known/oauth-protected-resource"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		f.mu.Lock()
		f.deletes++
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodGet:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	body, _ := io.ReadAll(r.Body)
	if json.Unmarshal(body, &req) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if req.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "fake-session" {
		http.Error(w, "missing session", http.StatusBadRequest)
		return
	}

	var result string
	switch req.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "fake-session")
		result = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"1"}}`
	case "tools/list":
		result = `{"tools":[{"name":"read_graph","inputSchema":{"type":"object"}}]}`
	case "tools/call":
		if f.mode == modeHang {
			<-r.Context().Done()
			return
		}
		result = `{"entities":[],"ok":true,"alpha":1,"beta":2}`
	default:
		result = `{}`
	}

	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)
	if f.mode == modeSSE {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{\"level\":\"info\",\"data\":\"working\"}}\n\n")
		_, _ = fmt.Fprintf(w, "id: %s-1\nevent: message\ndata: %s\n\n", strings.Trim(string(req.ID), `"`), msg)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, msg)
}

func httpCIConfig(agentBin, mode, url string) *CIConfig {
	return &CIConfig{
		Agent: AgentConfig{
			Command: []string{
				agentBin,
				"--mode", mode,
				"--tool", "read_graph",
				"--mcp-config", PlaceholderMCPConfig,
				"--prompt", PlaceholderPrompt,
			},
			Timeout: 60 * time.Second,
		},
		MCPServer: MCPServerConfig{
			Name:    "acme",
			URL:     url,
			Headers: map[string]string{"Authorization": "Bearer ${" + e2eTokenEnv + "}"},
		},
	}
}

func httpScenario(name string, gremlins ...string) ScenarioConfig {
	return ScenarioConfig{
		Name: name, Gremlins: gremlins, Seed: 7, Intensity: "certain", Prompt: "read the graph",
		Params: chaos.GremlinParams{MinTimeoutMs: 50, MaxTimeoutMs: 100},
	}
}

func isolateRunFiles(t *testing.T) (dataDir, tmpDir, token string) {
	t.Helper()
	dataDir, tmpDir, token = t.TempDir(), t.TempDir(), "e2e-"+uuid.NewString()
	t.Setenv("GREMLYN_DATA_DIR", dataDir)
	t.Setenv("TMPDIR", tmpDir)
	t.Setenv(e2eTokenEnv, token)
	return dataDir, tmpDir, token
}

func TestArenaCI_HTTP_ScoreDiscriminatesRobustFromFragile(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the reference agent and spawns it")
	}
	agentBin := buildRefAgent(t)

	for _, mode := range []fakeHTTPMode{modeJSON, modeSSE} {
		name := map[fakeHTTPMode]string{modeJSON: "json", modeSSE: "sse"}[mode]
		t.Run(name, func(t *testing.T) {
			isolateRunFiles(t)
			srv := newFakeHTTPMCP(t, mode)

			robust := runScenario(context.Background(), zerolog.Nop(), "",
				httpCIConfig(agentBin, "robust", srv.URL()), httpScenario("http-robust", "timeout"), false)
			fragile := runScenario(context.Background(), zerolog.Nop(), "",
				httpCIConfig(agentBin, "fragile", srv.URL()), httpScenario("http-fragile", "timeout"), false)
			t.Logf("robust:  overall=%d coverage=%s failures=%v", robust.Report.Overall, robust.Coverage, robust.Failures)
			t.Logf("fragile: overall=%d coverage=%s failures=%v", fragile.Report.Overall, fragile.Coverage, fragile.Failures)

			require.Positive(t, robust.Coverage.Injected)
			require.Positive(t, fragile.Coverage.Injected)
			assert.True(t, robust.Coverage.Complete(), "every injection is observed over HTTP")
			assert.Greater(t, robust.Report.Overall, fragile.Report.Overall)
			assert.Zero(t, robust.Upstream.Timeouts, "a gremlin timeout is not an upstream timeout")
			assert.GreaterOrEqual(t, srv.deleteCount(), 2, "the agent's DELETE reaches the server")
		})
	}
}

func TestArenaCI_HTTP_SameSeedSameScore(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the reference agent and spawns it")
	}
	isolateRunFiles(t)
	agentBin := buildRefAgent(t)
	srv := newFakeHTTPMCP(t, modeSSE)

	cfg := httpCIConfig(agentBin, "fragile", srv.URL())
	first := runScenario(context.Background(), zerolog.Nop(), "", cfg, httpScenario("seed-1", "corruption"), false)
	second := runScenario(context.Background(), zerolog.Nop(), "", cfg, httpScenario("seed-2", "corruption"), false)
	require.Positive(t, first.Coverage.Injected)
	assert.Equal(t, first.Report.Overall, second.Report.Overall)
	assert.Equal(t, first.Coverage, second.Coverage)
}

func TestArenaCI_HTTP_SecretsNeverReachAFile(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the reference agent and spawns it")
	}
	dataDir, tmpDir, token := isolateRunFiles(t)
	agentBin := buildRefAgent(t)
	srv := newFakeHTTPMCP(t, modeSSE)

	pathSecret := "p-" + uuid.NewString()
	t.Setenv("GREMLYN_E2E_PATH", pathSecret)

	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.DebugLevel)
	cfg := httpCIConfig(agentBin, "robust", srv.Server.URL+"/${GREMLYN_E2E_PATH}/mcp")
	envDump := filepath.Join(t.TempDir(), "agent-env")
	cfg.Agent.Command = append(cfg.Agent.Command, "--dump-env", envDump)
	res := runScenario(context.Background(), logger, "", cfg, httpScenario("secrets", "injection", "timeout"), true)

	agentEnv, err := os.ReadFile(envDump)
	require.NoError(t, err)
	dumped := string(agentEnv)
	require.True(t, strings.Contains(dumped, "PATH="), "the agent's environment was dumped")
	assert.False(t, strings.Contains(dumped, token), "the header value must not reach the agent process")
	assert.False(t, strings.Contains(dumped, pathSecret), "nor the URL's variable")
	assert.False(t, strings.Contains(dumped, e2eTokenEnv+"="), "the variable itself is removed")
	require.Positive(t, res.Coverage.Injected, "failures: %v", res.Failures)

	assert.Contains(t, srv.authHeaders(), "Bearer "+token, "the configured header reaches the server")

	outDir := t.TempDir()
	reportPath := filepath.Join(outDir, "report.json")
	result := CIResult{Scenarios: []ScenarioResult{res}, Passed: res.Passed()}
	var console bytes.Buffer
	require.NoError(t, writeCIResult(&console, reportPath, "json", result))
	writeScenarioText(&console, &res)
	var markdown bytes.Buffer
	renderMarkdown(&markdown, &result, nil)
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "comment.md"), markdown.Bytes(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "title.txt"), []byte(reportTitle(&result, nil)), 0o600))

	var scanned []string
	for _, root := range []string{dataDir, tmpDir, outDir} {
		require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned = append(scanned, filepath.Base(path))
			assert.NotContains(t, string(data), token, "the header value leaked into %s", path)
			assert.NotContains(t, string(data), pathSecret, "the URL path leaked into %s", path)
			return nil
		}))
	}
	for _, want := range []string{"session.json", "events.jsonl", "summary.json", "mcp.json", "report.json", "comment.md"} {
		assert.Contains(t, scanned, want, "%s must have been written and scanned", want)
	}
	assert.NotContains(t, console.String(), token)
	assert.NotContains(t, logs.String(), token)
	assert.NotContains(t, console.String(), pathSecret)
	assert.NotContains(t, logs.String(), pathSecret)

	sessions, err := os.ReadDir(filepath.Join(dataDir, "arena", "sessions"))
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	session, err := os.ReadFile(filepath.Join(dataDir, "arena", "sessions", sessions[0].Name(), "session.json"))
	require.NoError(t, err)
	assert.Contains(t, string(session), srv.Server.URL+"/…", "the server is recorded by scheme and host")
	assert.NotContains(t, string(session), "/mcp", "never by its path")
}

func TestArenaCI_HTTP_WritesTheClaudeCodeHTTPConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	require.NoError(t, writeHTTPMCPConfig(path, "acme", "http://127.0.0.1:4242/mcp"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{"mcpServers":{"acme":{"type":"http","url":"http://127.0.0.1:4242/mcp"}}}`, string(data),
		"the agent gets the local proxy and no header: secrets stay in the gremlyn process")
}

func TestArenaCI_HTTP_FailuresAreExplained(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the reference agent and spawns it")
	}
	agentBin := buildRefAgent(t)

	tests := []struct {
		name   string
		mode   fakeHTTPMode
		mutate func(*CIConfig)
		want   string
		check  func(t *testing.T, res ScenarioResult)
	}{
		{
			name: "a real upstream timeout",
			mode: modeHang,
			mutate: func(c *CIConfig) {
				c.MCPServer.RequestTimeout = 300 * time.Millisecond
			},
			want: "timed out 2 time(s), outside any gremlin",
			check: func(t *testing.T, res ScenarioResult) {
				assert.Equal(t, chaos.UpstreamStats{Timeouts: 2}, res.Upstream, "the call and the robust agent's retry")
				assert.Zero(t, res.Events, "a real timeout never becomes a gremlin event")
			},
		},
		{
			name: "a server on the legacy HTTP+SSE transport",
			mode: modeLegacy,
			want: "legacy HTTP+SSE transport",
		},
		{
			name: "credentials the server refuses",
			mode: modeJSON,
			mutate: func(c *CIConfig) {
				c.MCPServer.Headers = map[string]string{"Authorization": "Bearer wrong"}
			},
			want: "refused the request's credentials",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateRunFiles(t)
			srv := newFakeHTTPMCP(t, tt.mode)
			cfg := httpCIConfig(agentBin, "robust", srv.URL())
			if tt.mutate != nil {
				tt.mutate(cfg)
			}
			res := runScenario(context.Background(), zerolog.Nop(), "", cfg, httpScenario("fail", "corruption"), false)
			assert.False(t, res.Passed())
			assert.Contains(t, strings.Join(res.Failures, "\n"), tt.want)
			if tt.check != nil {
				tt.check(t, res)
			}
		})
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func postJSON(t *testing.T, url, session, body string) (sessionID, reply string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if session != "" {
		req.Header.Set("Mcp-Session-Id", session)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.Header.Get("Mcp-Session-Id"), string(data)
}

func TestWrapCmd_Upstream(t *testing.T) {
	dataDir, _, token := isolateRunFiles(t)
	srv := newFakeHTTPMCP(t, modeJSON)
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	summaryPath := filepath.Join(dir, "summary.json")

	cmd := NewWrapCmd(zerolog.Nop())
	stderr := &lockedBuffer{}
	cmd.SetOut(io.Discard)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{
		"--upstream", srv.URL(),
		"--upstream-header", "Authorization: Bearer ${" + e2eTokenEnv + "}",
		"--chaos-gremlins", "corruption",
		"--chaos-intensity", "certain",
		"--chaos-events", eventsPath,
		"--chaos-summary", summaryPath,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.ExecuteContext(ctx) }()

	var local string
	require.Eventually(t, func() bool {
		out := stderr.String()
		if i := strings.Index(out, " at http://"); i >= 0 {
			local = strings.TrimSpace(out[i+4:])
			return true
		}
		return false
	}, 10*time.Second, 10*time.Millisecond, "the proxy prints where it listens")

	session, _ := postJSON(t, local, "", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	require.Equal(t, "fake-session", session)
	_, body := postJSON(t, local, session, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_graph","arguments":{}}}`)
	assert.NotContains(t, body, `"alpha"`, "corruption at certain intensity rewrites the tool result")

	cancel()
	require.NoError(t, <-done)

	summary, err := chaos.ReadSummary(summaryPath)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Coverage.Injected)
	assert.Equal(t, srv.Server.URL+"/…", summary.ServerURL)
	assert.Empty(t, summary.ServerCommand)

	events, _, err := chaos.ReadEvents(eventsPath)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "corruption", events[0].GremlinType)

	for _, root := range []string{dataDir, dir} {
		require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, string(data), token, path)
			return nil
		}))
	}
	assert.NotContains(t, stderr.String(), token)
}

func TestWrapCmd_UpstreamFlagValidation(t *testing.T) {
	t.Setenv("GREMLYN_DATA_DIR", t.TempDir())
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"an upstream and a command", []string{"--upstream", "https://mcp.example.com/mcp", "--", "srv"}, "drop the command"},
		{"plain http off loopback", []string{"--upstream", "http://mcp.example.com/mcp"}, "plain http"},
		{"a header without an upstream", []string{"--upstream-header", "X-A: b", "--", "srv"}, "with --upstream only"},
		{"a listen address without an upstream", []string{"--listen", "127.0.0.1:0", "--", "srv"}, "with --upstream only"},
		{"a non-loopback listen address", []string{"--upstream", "https://mcp.example.com/mcp", "--listen", "0.0.0.0:0"}, "not a loopback address"},
		{"a missing variable", []string{"--upstream", "https://mcp.example.com/mcp", "--upstream-header", "Authorization: ${GREMLYN_NOPE}"}, "GREMLYN_NOPE is not set"},
		{"a malformed header", []string{"--upstream", "https://mcp.example.com/mcp", "--upstream-header", "nocolon"}, "Name: value"},
		{"a zero timeout", []string{"--upstream", "https://mcp.example.com/mcp", "--upstream-timeout", "0s"}, "must be positive"},
		{"a negative timeout", []string{"--upstream", "https://mcp.example.com/mcp", "--upstream-timeout=-1s"}, "must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewWrapCmd(zerolog.Nop())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func readOnlySession(t *testing.T, dataDir string) map[string]any {
	t.Helper()
	dirs, err := os.ReadDir(filepath.Join(dataDir, "arena", "sessions"))
	require.NoError(t, err)
	require.Len(t, dirs, 1)
	data, err := os.ReadFile(filepath.Join(dataDir, "arena", "sessions", dirs[0].Name(), "session.json"))
	require.NoError(t, err)
	var session map[string]any
	require.NoError(t, json.Unmarshal(data, &session))
	return session
}

func TestWrapCmd_UpstreamThatCannotListenFailsTheSession(t *testing.T) {
	dataDir, _, _ := isolateRunFiles(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })

	cmd := NewWrapCmd(zerolog.Nop())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{
		"--upstream", "https://mcp.example.com/mcp",
		"--listen", busy.Addr().String(),
		"--chaos-gremlins", "timeout",
	})
	require.Error(t, cmd.Execute())
	assert.Equal(t, "failed", readOnlySession(t, dataDir)["status"], "a run that never started is not left running")
}
