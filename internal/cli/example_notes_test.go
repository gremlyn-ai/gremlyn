package cli

import (
	"bufio"
	"context"
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
	assert.Contains(t, byName["timeout-on-create"].Prompt, "Create a note")
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

	duplicated := run("timeout-on-create", "--mode", "robust", "--tool", "create_note")
	assert.Equal(t, 1, duplicated.Report.DuplicateWrites)
	checked := run("timeout-on-create", "--mode", "checker", "--tool", "create_note", "--then-tool", "list_notes")
	assert.Zero(t, checked.Report.DuplicateWrites)
	assert.Greater(t, checked.Report.Overall, duplicated.Report.Overall)

	for _, res := range []ScenarioResult{hijacked, ignored, duplicated, checked} {
		assert.Zero(t, res.Report.UnknownToolInjections, "%s: every tool of the example is annotated", res.Name)
		assert.Len(t, res.Tools, 3)
		assert.Equal(t, chaos.Coverage{Injected: res.Coverage.Injected, Observed: res.Coverage.Injected}, res.Coverage)
	}
}
