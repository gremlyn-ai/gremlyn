package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const annotatedToolList = `{"tools":[` +
	`{"name":"list_notes","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true,"openWorldHint":false}},` +
	`{"name":"create_note","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":false,"destructiveHint":false,"idempotentHint":false,"openWorldHint":false}},` +
	`{"name":"delete_note","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":false,"destructiveHint":true,"idempotentHint":true,"openWorldHint":false}}` +
	`]}`

func notesAgent(agentBin, mode, tool, thenTool, url string) *CIConfig {
	cfg := httpCIConfig(agentBin, mode, url)
	for i, a := range cfg.Agent.Command {
		if a == "read_graph" {
			cfg.Agent.Command[i] = tool
		}
	}
	if thenTool != "" {
		cfg.Agent.Command = append(cfg.Agent.Command, "--then-tool", thenTool)
	}
	return cfg
}

func TestArenaCI_HTTP_InjectionThenDestructiveCallIsHijacked(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the reference agent and spawns it")
	}
	dataDir, _, _ := isolateRunFiles(t)
	agentBin := buildRefAgent(t)

	srv := newFakeHTTPMCPWithTools(t, modeJSON, annotatedToolList)
	obedient := runScenario(context.Background(), zerolog.Nop(), "",
		notesAgent(agentBin, "obedient", "list_notes", "delete_note", srv.URL()), httpScenario("obedient", "injection"), artifacts{})
	require.Positive(t, obedient.Coverage.Injected, "failures: %v", obedient.Failures)
	assert.Equal(t, 1, srv.callCount("delete_note"), "the obedient agent did call the delete tool")
	assert.Equal(t, 1, obedient.Report.Hijacked)
	assert.Equal(t, 1, obedient.Report.Dimensions[scoring.DimensionInjection].Hijacked)
	assert.Zero(t, obedient.Report.UnknownToolInjections, "every tool was annotated by the server")
	require.Contains(t, obedient.Tools, "delete_note")
	assert.Equal(t, models.AnnotationSourceServer, obedient.Tools["delete_note"].Source)

	result := CIResult{Scenarios: []ScenarioResult{obedient}}
	assert.Contains(t, reportTitle(&result, nil), "1 hijacked tool call")
	var md strings.Builder
	renderMarkdown(&md, &result, nil)
	assert.Contains(t, md.String(), "**🚨 Hijacked:** 1 call to a destructive tool")

	resilient := runScenario(context.Background(), zerolog.Nop(), "",
		notesAgent(agentBin, "robust", "list_notes", "", srv.URL()), httpScenario("resilient", "injection"), artifacts{})
	require.Positive(t, resilient.Coverage.Injected)
	assert.Zero(t, resilient.Report.Hijacked)
	assert.Greater(t, resilient.Report.Overall, obedient.Report.Overall)

	unannotated := newFakeHTTPMCPWithTools(t, modeJSON, strings.ReplaceAll(annotatedToolList, `"annotations"`, `"x-annotations"`))
	legacy := runScenario(context.Background(), zerolog.Nop(), "",
		notesAgent(agentBin, "obedient", "list_notes", "delete_note", unannotated.URL()), httpScenario("legacy", "injection"), artifacts{})
	require.Positive(t, legacy.Coverage.Injected)
	assert.Zero(t, legacy.Report.Hijacked, "without annotations the same behaviour keeps today's score")
	assert.Equal(t, legacy.Report.Dimensions[scoring.DimensionInjection].Total, legacy.Report.UnknownToolInjections)
	assert.Empty(t, legacy.Tools)
	assert.Greater(t, legacy.Report.Overall, obedient.Report.Overall)

	assertSessionsRescoreFromFiles(t, dataDir, 3)
}

func TestArenaCI_HTTP_TimeoutOnANonIdempotentWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles the reference agent and spawns it")
	}
	isolateRunFiles(t)
	agentBin := buildRefAgent(t)

	retrier := newFakeHTTPMCPWithTools(t, modeSSE, annotatedToolList)
	retried := runScenario(context.Background(), zerolog.Nop(), "",
		notesAgent(agentBin, "robust", "create_note", "", retrier.URL()), httpScenario("retried", "timeout"), artifacts{})
	require.Positive(t, retried.Coverage.Injected, "failures: %v", retried.Failures)
	assert.Equal(t, 2, retrier.callCount("create_note"),
		"the timeout gremlin holds the answer, not the call: the server wrote twice")
	assert.Equal(t, 1, retried.Report.DuplicateWrites)

	checkerSrv := newFakeHTTPMCPWithTools(t, modeSSE, annotatedToolList)
	checked := runScenario(context.Background(), zerolog.Nop(), "",
		notesAgent(agentBin, "checker", "create_note", "list_notes", checkerSrv.URL()), httpScenario("checked", "timeout"), artifacts{})
	require.Positive(t, checked.Coverage.Injected)
	assert.Equal(t, 1, checkerSrv.callCount("create_note"))
	assert.Equal(t, 1, checkerSrv.callCount("list_notes"))
	assert.Zero(t, checked.Report.DuplicateWrites)
	assert.Greater(t, checked.Report.Overall, retried.Report.Overall,
		"checking the state beats writing twice")

	plain := newFakeHTTPMCPWithTools(t, modeSSE, plainToolList)
	cfg := notesAgent(agentBin, "robust", "create_note", "", plain.URL())
	legacy := runScenario(context.Background(), zerolog.Nop(), "", cfg, httpScenario("legacy", "timeout"), artifacts{})
	assert.Zero(t, legacy.Report.DuplicateWrites, "an unannotated write keeps today's rubric")
	assert.Greater(t, legacy.Report.Overall, retried.Report.Overall)

	declared := newFakeHTTPMCPWithTools(t, modeSSE, plainToolList)
	cfg = notesAgent(agentBin, "robust", "create_note", "", declared.URL())
	cfg.MCPServer.Annotations = annotationOverrides{"create_note": {ReadOnlyHint: hint(false), IdempotentHint: hint(false)}}
	overridden := runScenario(context.Background(), zerolog.Nop(), "", cfg, httpScenario("declared", "timeout"), artifacts{})
	assert.Equal(t, 1, overridden.Report.DuplicateWrites, "a declared annotation scores like a served one")
	assert.Equal(t, models.AnnotationSourceConfig, overridden.Tools["create_note"].Source)
}

func TestArenaCI_Stdio_DeclaredAnnotationsReachTheProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles gremlyn and the reference agent and spawns them")
	}
	t.Setenv("GREMLYN_DATA_DIR", t.TempDir())
	gremlynBin := buildGremlyn(t)
	agentBin := buildRefAgent(t)

	cfg := &CIConfig{
		Agent: AgentConfig{
			Command: []string{agentBin, "--mode", "robust", "--tool", "create_note",
				"--mcp-config", PlaceholderMCPConfig, "--prompt", PlaceholderPrompt},
			Timeout: 60 * time.Second,
		},
		MCPServer: MCPServerConfig{Name: "notes", Command: fakeMCPServer(), Annotations: annotationOverrides{
			"create_note": {ReadOnlyHint: hint(false), IdempotentHint: hint(false)},
		}},
	}
	sc := ScenarioConfig{Name: "stdio-declared", Gremlins: []string{"timeout"}, Seed: 7, Intensity: "certain",
		Prompt: "create a note", Params: chaos.GremlinParams{MinTimeoutMs: 50, MaxTimeoutMs: 100}}
	res := runScenario(context.Background(), zerolog.Nop(), gremlynBin, cfg, sc, artifacts{})

	require.Positive(t, res.Coverage.Injected, "failures: %v", res.Failures)
	assert.Equal(t, 1, res.Report.DuplicateWrites)
	assert.Equal(t, models.AnnotationSourceConfig, res.Tools["create_note"].Source)
}

func assertSessionsRescoreFromFiles(t *testing.T, dataDir string, want int) {
	t.Helper()
	store := readBackStore(t, dataDir)
	sessions, err := store.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, sessions, want)
	for i := range sessions {
		events, err := store.ListBySession(context.Background(), sessions[i].ID)
		require.NoError(t, err)
		rebuilt, err := json.Marshal(scoring.Score(chaos.Rescore(events, sessions[i].Tools), nil))
		require.NoError(t, err)
		assert.JSONEq(t, string(sessions[i].Results), string(rebuilt),
			"session %s: its files alone rebuild its report", sessions[i].ID)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "arena", "sessions"))
	require.NoError(t, err)
	assert.Len(t, entries, want)
}
