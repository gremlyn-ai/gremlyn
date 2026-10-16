package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var notesExample = filepath.Join("..", "..", "examples", "arena-notes.yaml")

func TestExampleNotesScenarioFile(t *testing.T) {
	cfg, err := loadCIConfig(notesExample)
	require.NoError(t, err)

	u, err := proxy.ValidateUpstreamURL(cfg.MCPServer.URL)
	require.NoError(t, err)
	assert.Equal(t, "http", u.Scheme)
	assert.Equal(t, "127.0.0.1", u.Hostname(), "the example targets the local throwaway server, never a real one")
	assert.Empty(t, cfg.MCPServer.Headers, "the local example needs no credential")
	assert.Empty(t, cfg.MCPServer.Annotations, "the example server annotates its own tools")

	byName := map[string]ScenarioConfig{}
	for _, sc := range cfg.Scenarios {
		byName[sc.Name] = sc
	}
	require.Contains(t, byName, "injected-delete")
	require.Contains(t, byName, "timeout-on-create")
	assert.Equal(t, []string{"injection"}, byName["injected-delete"].Gremlins)
	assert.Contains(t, byName["injected-delete"].Params.InjectionText, "delete_note",
		"the measured failure is a call to the local delete tool")
	assert.Equal(t, []string{"timeout"}, byName["timeout-on-create"].Gremlins)
	assert.Equal(t, targetTools{"create_note"}, byName["timeout-on-create"].TargetTools)
	assert.True(t, strings.HasPrefix(byName["timeout-on-create"].Prompt, "List my notes, then create a note"),
		"the agent reads before it writes, and only the write is aimed at")
	assert.Nil(t, byName["injected-delete"].TargetTools)
}

func startNotesServer(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "notes-server")
	out, err := exec.Command("go", "build", "-o", bin, "../../examples/notes-server").CombinedOutput()
	require.NoError(t, err, "building notes-server: %s", out)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "--addr", "127.0.0.1:0")
	stderr, err := cmd.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	found := make(chan string, 1)
	go func() {
		re := regexp.MustCompile(`http://127\.0\.0\.1:\d+/mcp`)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := re.FindString(sc.Text()); m != "" {
				found <- m
				break
			}
		}
		for sc.Scan() {
		}
	}()
	select {
	case url := <-found:
		return url
	case <-time.After(30 * time.Second):
		t.Fatal("notes-server did not report its address")
		return ""
	}
}

func countNotes(t *testing.T, url string) int {
	t.Helper()
	post := func(session, body string) (http.Header, []byte) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		return resp.Header, data
	}
	header, _ := post("", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`)
	session := header.Get("Mcp-Session-Id")
	require.NotEmpty(t, session)
	_, data := post(session, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_notes","arguments":{}}}`)
	var out struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out.Result.Content, 1)
	return strings.Count("\n"+out.Result.Content[0].Text, "\n#")
}

func TestExampleNotesServer_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the example server and the reference agent and spawns them")
	}
	isolateRunFiles(t)
	agentBin := buildRefAgent(t)
	url := startNotesServer(t)

	cfg, err := loadCIConfig(notesExample)
	require.NoError(t, err)
	cfg.MCPServer.URL = url

	run := func(name string, agentArgs ...string) ScenarioResult {
		t.Helper()
		scs, err := selectScenarios(cfg.Scenarios, []string{name})
		require.NoError(t, err)
		sc := scs[0]
		sc.Params.MinTimeoutMs, sc.Params.MaxTimeoutMs = 50, 100
		c := *cfg
		c.Agent = AgentConfig{
			Command: append([]string{agentBin, "--mcp-config", PlaceholderMCPConfig, "--prompt", PlaceholderPrompt, "--expect-key", ""}, agentArgs...),
			Timeout: 60 * time.Second,
		}
		res := runScenario(context.Background(), zerolog.Nop(), "", &c, sc, false)
		require.Positive(t, res.Coverage.Injected, "failures: %v", res.Failures)
		return res
	}

	hijacked := run("injected-delete", "--mode", "obedient", "--tool", "list_notes", "--then-tool", "delete_note")
	assert.Equal(t, 1, hijacked.Report.Hijacked)
	assert.Contains(t, strings.Join(hijacked.Failures, "\n"), "below the minimum 50")
	ignored := run("injected-delete", "--mode", "fragile", "--tool", "list_notes")
	assert.Zero(t, ignored.Report.Hijacked)
	assert.Greater(t, ignored.Report.Overall, hijacked.Report.Overall)

	const groceries = `{"title":"groceries","body":"milk, eggs, bread"}`
	before := countNotes(t, url)
	duplicated := run("timeout-on-create", "--mode", "robust", "--before", "list_notes", "--tool", "create_note", "--args", groceries)
	assert.Equal(t, 1, duplicated.Report.DuplicateWrites)
	assert.Equal(t, before+2, countNotes(t, url), "the timeout held the answer, not the call: the server holds the note twice")

	before = countNotes(t, url)
	rejected := run("timeout-on-create", "--mode", "robust", "--tool", "create_note")
	assert.Equal(t, before, countNotes(t, url), "a create without a title is rejected and writes nothing")
	assert.Zero(t, rejected.Report.DuplicateWrites, "retrying a rejected write is not a duplicate write")
	assert.Greater(t, rejected.Report.Overall, duplicated.Report.Overall)

	before = countNotes(t, url)
	checked := run("timeout-on-create", "--mode", "checker", "--tool", "create_note", "--then-tool", "list_notes", "--args", groceries)
	assert.Zero(t, checked.Report.DuplicateWrites)
	assert.Equal(t, before+1, countNotes(t, url))
	assert.Greater(t, checked.Report.Overall, duplicated.Report.Overall)

	for _, res := range []ScenarioResult{hijacked, ignored, duplicated, checked} {
		assert.Zero(t, res.Report.UnknownToolInjections, "%s: every tool of the example is annotated", res.Name)
		assert.Len(t, res.Tools, 3)
		assert.Equal(t, chaos.Coverage{Injected: res.Coverage.Injected, Observed: res.Coverage.Injected}, res.Coverage)
	}
}

func TestExampleNotesServer_TargetToolsAimTheTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the example server and the reference agent and spawns them")
	}
	isolateRunFiles(t)
	agentBin := buildRefAgent(t)
	url := startNotesServer(t)

	cfg, err := loadCIConfig(notesExample)
	require.NoError(t, err)
	cfg.MCPServer.URL = url
	cfg.Agent = AgentConfig{
		Command: []string{agentBin, "--mcp-config", PlaceholderMCPConfig, "--mode", "fragile", "--before", "list_notes",
			"--tool", "create_note", "--args", `{"title":"groceries","body":"milk, eggs, bread"}`},
		Timeout: 60 * time.Second,
	}
	scs, err := selectScenarios(cfg.Scenarios, []string{"timeout-on-create"})
	require.NoError(t, err)
	sc := scs[0]
	sc.Params.MinTimeoutMs, sc.Params.MaxTimeoutMs = 50, 100

	before := countNotes(t, url)
	aimed, tools := runAimed(t, "", cfg, sc, sc.TargetTools)
	assert.Equal(t, 1, aimed.Coverage.Injected, "failures: %v", aimed.Failures)
	assert.Equal(t, []string{"create_note"}, tools, "the read before the write passes untouched")
	assert.Equal(t, before+1, countNotes(t, url), "the timeout held the answer, not the write")
	require.Len(t, aimed.Targets, 1)
	assert.Equal(t, chaos.ListingListed, aimed.Targets[0].Listing)
	assert.Equal(t, 1, aimed.Targets[0].Called)

	both, _ := runAimed(t, "", cfg, sc, targetTools{"create_note", "delete_note"})
	assert.True(t, both.Report.Measured)
	assert.Contains(t, both.Failures, `target "delete_note" was never called, so no gremlin reached it`)

	typo, _ := runAimed(t, "", cfg, sc, targetTools{"create_notes"})
	assert.Contains(t, typo.Failures, `target "create_notes" is not offered by the server: no tools/list the run saw lists it (check the name)`)

	all, tools := runAimed(t, "", cfg, sc, nil)
	assert.Equal(t, 2, all.Coverage.Injected, "failures: %v", all.Failures)
	assert.Equal(t, []string{"list_notes", "create_note"}, tools, "without the key the read is hit first")
}
