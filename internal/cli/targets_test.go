package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const targetsConfig = `agent:
  command: [sh, -c, "touch %s # {{mcp_config}}"]
mcp_server:
  command: ["true"]
scenarios:
  - name: s1
    gremlins: [timeout]
%s
`

func TestLoadCIConfig_TargetTools(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    targetTools
		wantErr string
	}{
		{"absent", "", nil, ""},
		{"null", "    target_tools:", nil, ""},
		{"tilde", "    target_tools: ~", nil, ""},
		{"case-distinct names", "    target_tools: [create_note, Create_Note]", targetTools{"create_note", "Create_Note"}, ""},
		{"empty list", "    target_tools: []", nil, `scenario "s1": target_tools: the list is empty`},
		{"an int", "    target_tools: [create_note, 1]", nil, "line 8: target_tools[1] must be a tool name (a string), got !!int"},
		{"a bool", "    target_tools: [true]", nil, "target_tools[0] must be a tool name (a string), got !!bool"},
		{"a mapping", "    target_tools: [{a: b}]", nil, "target_tools[0] must be a tool name (a string), got !!map"},
		{"a null item", "    target_tools: [~]", nil, "target_tools[0] must be a tool name (a string), got !!null"},
		{"a scalar", "    target_tools: create_note", nil, "line 8: target_tools is a list of tool names"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			marker, path := filepath.Join(dir, "agent-ran"), filepath.Join(dir, "arena.yaml")
			require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, targetsConfig, marker, tt.line), 0o600))
			if tt.wantErr == "" {
				cfg, err := loadCIConfig(path)
				require.NoError(t, err)
				assert.Equal(t, tt.want, cfg.Scenarios[0].TargetTools)
				return
			}
			cmd := NewArenaCICmd(zerolog.Nop())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--config", path})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NoFileExists(t, marker, "a config error stops the run before the agent starts")
		})
	}
}

func TestWrapCmd_RejectsTargetsItCannotEnforce(t *testing.T) {
	t.Setenv("GREMLYN_DATA_DIR", t.TempDir())
	const flag = "--chaos-target-tool"
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"without gremlins", []string{flag, "create_note", "--", "true"}, "applies with --chaos-gremlins only"},
		{"an empty name", []string{"--chaos-gremlins", "timeout", flag, "", "--", "true"}, "configuring chaos: target tools: entry 1 is empty"},
		{"a duplicate", []string{"--chaos-gremlins", "timeout", flag, "a", flag, "a", "--", "true"}, `"a" is listed twice`},
		{"a padded name", []string{"--chaos-gremlins", "timeout", flag, "a ", "--", "true"}, "leading or trailing whitespace"},
		{"the fake tool", []string{"--chaos-gremlins", "hallucination", flag, "nonexistent_tool", "--", "true"}, "the hallucination gremlin invents"},
		{"a duplicate behind --upstream", []string{"--chaos-gremlins", "timeout", flag, "a", flag, "a", "--upstream", "http://127.0.0.1:1/mcp"}, `"a" is listed twice`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewWrapCmd(zerolog.Nop())
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestReplay_CarriesTheTargetTools(t *testing.T) {
	summary := chaos.SessionSummary{Gremlins: []string{"timeout"}, Seed: 1, ServerCommand: []string{"./srv"}}
	out, _, err := runReplay(t, writeSummary(t, summary))
	require.NoError(t, err)
	assert.NotContains(t, out, "--chaos-target-tool", "an untargeted summary replays as before")

	summary.TargetTools = []string{"create_note", "my tool"}
	out, _, err = runReplay(t, writeSummary(t, summary))
	require.NoError(t, err)
	assert.Contains(t, out, "--chaos-target-tool create_note \\\n  --chaos-target-tool 'my tool' \\\n  -- ./srv")
}

func TestChaosRun_RecordsTheTargets(t *testing.T) {
	for _, targets := range [][]string{{"create_note"}, nil} {
		t.Run(fmt.Sprint(targets), func(t *testing.T) {
			dataDir, summaryPath := t.TempDir(), filepath.Join(t.TempDir(), "summary.json")
			t.Setenv("GREMLYN_DATA_DIR", dataDir)
			run, err := newChaosRun(context.Background(), chaosSetup{
				Gremlins: []string{"timeout"}, Seed: 1, Intensity: "certain", TargetTools: targets, SummaryPath: summaryPath,
			}, zerolog.Nop())
			require.NoError(t, err)
			run.finish(context.Background(), nil, true, chaos.UpstreamStats{})
			run.close()

			summary, err := chaos.ReadSummary(summaryPath)
			require.NoError(t, err)
			assert.Equal(t, targets, summary.TargetTools)
			sessions, err := readBackStore(t, dataDir).ListSessions(context.Background())
			require.NoError(t, err)
			require.Len(t, sessions, 1)
			var recipe chaosRecipe
			require.NoError(t, json.Unmarshal(sessions[0].Config, &recipe))
			assert.Equal(t, targets, recipe.TargetTools)
			assert.Equal(t, targets != nil, bytes.Contains(sessions[0].Config, []byte(`"target_tools"`)), "an absent key is not written")
		})
	}
}

func runAimed(t *testing.T, self string, cfg *CIConfig, sc ScenarioConfig, targets targetTools) (res ScenarioResult, tools []string) {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("GREMLYN_DATA_DIR", dataDir)
	sc.TargetTools = targets
	res = runScenario(context.Background(), zerolog.Nop(), self, cfg, sc, false)

	store := readBackStore(t, dataDir)
	sessions, err := store.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	events, err := store.ListBySession(context.Background(), sessions[0].ID)
	require.NoError(t, err)
	tools = make([]string, 0, len(events))
	for _, e := range events {
		var d models.EventDetails
		require.NoError(t, json.Unmarshal(e.Details, &d))
		tools = append(tools, d.Tool)
	}
	return res, tools
}

func TestArenaCI_Stdio_TargetToolsReachTheProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles gremlyn and the reference agent and spawns them")
	}
	gremlynBin := buildGremlyn(t)
	cfg := &CIConfig{
		Agent: AgentConfig{Command: []string{buildRefAgent(t), "--mode", "fragile", "--before", "list_notes",
			"--tool", "read_graph", "--mcp-config", PlaceholderMCPConfig}, Timeout: 60 * time.Second},
		MCPServer: MCPServerConfig{Name: "memory", Command: fakeMCPServer()},
	}
	sc := httpScenario("stdio-targets", "timeout")

	aimed, tools := runAimed(t, gremlynBin, cfg, sc, targetTools{"read_graph"})
	assert.Equal(t, 1, aimed.Coverage.Injected, "failures: %v", aimed.Failures)
	assert.Equal(t, []string{"read_graph"}, tools)
	assert.Equal(t, []chaos.TargetResult{{
		TargetStat: chaos.TargetStat{Name: "read_graph", Called: 1, Injected: 1, Listing: chaos.ListingUnverified},
		Scored:     1, State: chaos.TargetScored,
	}}, aimed.Targets, "the counts cross from the wrap process through summary.json")

	all, tools := runAimed(t, gremlynBin, cfg, sc, nil)
	assert.Equal(t, 2, all.Coverage.Injected, "failures: %v", all.Failures)
	assert.Equal(t, []string{"list_notes", "read_graph"}, tools)
	assert.Nil(t, all.Targets)
}

func TestWriteScenarioText_Targets(t *testing.T) {
	res := ScenarioResult{Name: "s", Targets: []chaos.TargetResult{
		{TargetStat: chaos.TargetStat{Name: "create_note", Called: 2, Injected: 1, Listing: chaos.ListingListed}, Scored: 1, State: chaos.TargetScored},
	}}
	var b strings.Builder
	writeScenarioText(&b, &res)
	assert.Contains(t, b.String(), "  target:     create_note scored (called 2, injected 1, scored 1, listed)\n")
}
