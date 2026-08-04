package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Config validation ──

func TestValidateCIConfig(t *testing.T) {
	valid := func() CIConfig {
		return CIConfig{
			Agent:     AgentConfig{Command: []string{"agent", "--mcp-config", PlaceholderMCPConfig}},
			MCPServer: MCPServerConfig{Command: []string{"server"}},
			Scenarios: []ScenarioConfig{{Name: "s1", Gremlins: []string{"corruption"}}},
		}
	}

	tests := []struct {
		name    string
		mutate  func(*CIConfig)
		wantErr string
	}{
		{"valid", func(*CIConfig) {}, ""},
		{"no agent command", func(c *CIConfig) { c.Agent.Command = nil }, "agent.command is required"},
		{
			// Without the placeholder the agent talks to its own MCP servers, never
			// crosses the proxy, and the run measures nothing while looking fine.
			"agent command without the mcp_config placeholder",
			func(c *CIConfig) { c.Agent.Command = []string{"agent", "-p", "hi"} },
			"must contain {{mcp_config}}",
		},
		{"no server command", func(c *CIConfig) { c.MCPServer.Command = nil }, "mcp_server.command is required"},
		{"no scenarios", func(c *CIConfig) { c.Scenarios = nil }, "at least one scenario"},
		{"scenario without a name", func(c *CIConfig) { c.Scenarios[0].Name = "" }, "name is required"},
		{"scenario without gremlins", func(c *CIConfig) { c.Scenarios[0].Gremlins = nil }, "at least one gremlin"},
		{"unknown gremlin", func(c *CIConfig) { c.Scenarios[0].Gremlins = []string{"nope"} }, "unknown gremlin"},
		{"unknown intensity", func(c *CIConfig) { c.Scenarios[0].Intensity = "extreme" }, "unknown intensity"},
		{"duplicate gremlin", func(c *CIConfig) {
			c.Scenarios[0].Gremlins = []string{"corruption", "corruption"}
		}, "more than once"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid()
			tt.mutate(&cfg)
			err := validateCIConfig(&cfg)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Positive(t, cfg.Agent.Timeout, "a default timeout must be filled in")
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// ── Threshold logic ──

func TestCheckThresholds(t *testing.T) {
	full := func(overall int) ScenarioResult {
		return ScenarioResult{
			Report:   scoring.ResilienceReport{Overall: overall},
			Coverage: coverage(2, 2, 0),
		}
	}

	t.Run("meets the minimum", func(t *testing.T) {
		assert.Empty(t, checkThresholds(ThresholdConfig{MinOverall: 70}, full(80)))
	})

	t.Run("below the minimum", func(t *testing.T) {
		f := checkThresholds(ThresholdConfig{MinOverall: 70}, full(40))
		require.Len(t, f, 1)
		assert.Contains(t, f[0], "below the minimum")
	})

	// The trap found while validating the headless contract: an agent can exit 0
	// having never called a tool. Nothing was tested, so this must not pass.
	t.Run("no gremlin crossed is a failure, not a pass", func(t *testing.T) {
		res := ScenarioResult{
			Report:   scoring.ResilienceReport{Overall: 100},
			Coverage: coverage(0, 0, 0),
		}
		f := checkThresholds(ThresholdConfig{MinOverall: 70}, res)
		require.Len(t, f, 1)
		assert.Contains(t, f[0], "no gremlin was ever crossed")
	})

	t.Run("incomplete coverage is reported", func(t *testing.T) {
		res := ScenarioResult{
			Report:   scoring.ResilienceReport{Overall: 90},
			Coverage: coverage(3, 1, 2),
		}
		f := checkThresholds(ThresholdConfig{}, res)
		require.NotEmpty(t, f)
		assert.Contains(t, f[0], "coverage incomplete")
	})

	// A threshold on a dimension that never ran would otherwise pass silently.
	t.Run("threshold on an unexercised dimension fails", func(t *testing.T) {
		res := full(90)
		f := checkThresholds(ThresholdConfig{
			MinDimension: map[scoring.Dimension]int{"data_integrity": 60},
		}, res)
		require.Len(t, f, 1)
		assert.Contains(t, f[0], "never exercised")
	})
}

// ── MCP config generation ──

// The generated config must point the agent at this binary in chaos mode, or the
// gremlins are not in the traffic path at all.
func TestWriteMCPConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	err := writeMCPConfig(path, "/usr/bin/gremlyn",
		MCPServerConfig{Name: "memory", Command: []string{"npx", "-y", "server-memory"}},
		ScenarioConfig{Gremlins: []string{"corruption", "latency"}, Intensity: "high"},
		42, "/tmp/e.jsonl", "/tmp/s.json")
	require.NoError(t, err)

	var got struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &got))

	srv, ok := got.MCPServers["memory"]
	require.True(t, ok, "the server must be registered under its configured name")
	assert.Equal(t, "/usr/bin/gremlyn", srv.Command)

	joined := strings.Join(srv.Args, " ")
	assert.Contains(t, joined, "wrap")
	assert.Contains(t, joined, "--chaos-gremlins corruption,latency")
	assert.Contains(t, joined, "--chaos-seed 42")
	assert.Contains(t, joined, "--chaos-intensity high")
	assert.Contains(t, joined, "--chaos-events /tmp/e.jsonl")
	assert.Contains(t, joined, "--chaos-summary /tmp/s.json")

	// The real server must come after the separator, so wrap does not eat its args.
	sep := indexOf(srv.Args, "--")
	require.Positive(t, sep, "the child command must be separated by --")
	assert.Equal(t, []string{"npx", "-y", "server-memory"}, srv.Args[sep+1:])
}

func TestSubstitute(t *testing.T) {
	got := substitute(
		[]string{"agent", "-p", PlaceholderPrompt, "--mcp-config", PlaceholderMCPConfig},
		map[string]string{PlaceholderPrompt: "do the thing", PlaceholderMCPConfig: "/tmp/mcp.json"},
	)
	assert.Equal(t, []string{"agent", "-p", "do the thing", "--mcp-config", "/tmp/mcp.json"}, got)
}

// ── End to end, with a scripted reference agent ──

// buildRefAgent compiles the scripted agent once per test binary.
func buildRefAgent(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "refagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/refagent")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building refagent: %s", out)
	return bin
}

// buildGremlyn compiles the CLI, because the generated MCP config points the
// agent at the gremlyn binary rather than at this test process.
func buildGremlyn(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gremlyn")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/gremlyn")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building gremlyn: %s", out)
	return bin
}

// fakeMCPServer is a shell MCP server: it answers every request with a result,
// so any bad result the agent sees came from a gremlin.
func fakeMCPServer() []string {
	const script = `while IFS= read -r line; do
	  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
	  [ -n "$id" ] && printf '{"jsonrpc":"2.0","id":%s,"result":{"entities":[],"ok":true,"alpha":1,"beta":2}}\n' "$id"
	done`
	return []string{"sh", "-c", script}
}

func runCI(t *testing.T, gremlynBin, agentBin, mode string, gremlins []string) ScenarioResult {
	t.Helper()

	cfg := &CIConfig{
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
		MCPServer: MCPServerConfig{Name: "memory", Command: fakeMCPServer()},
	}
	sc := ScenarioConfig{
		Name:      "e2e-" + mode,
		Gremlins:  gremlins,
		Seed:      7,
		Intensity: "certain",
		Prompt:    "read the graph",
	}
	return runScenario(context.Background(), zerolog.Nop(), gremlynBin, cfg, sc, false)
}

// THE P0.3 GATE.
//
// Same seed, same gremlins, two agents whose only difference is whether they
// react to a broken tool result. If the scores do not separate, the score is not
// measuring the agent and everything built on it is decorative.
func TestArenaCI_ScoreDiscriminatesRobustFromFragile(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes and compiles two binaries")
	}
	gremlynBin := buildGremlyn(t)
	agentBin := buildRefAgent(t)

	// timeout, not corruption. A field-dropping gremlin removes each key with a
	// probability, so whether the agent CAN detect the fault varies run to run —
	// which makes it the wrong instrument for a gate. A timeout always yields a
	// JSON-RPC error, so "did the agent react" is the only variable left.
	robust := runCI(t, gremlynBin, agentBin, "robust", []string{"timeout"})
	fragile := runCI(t, gremlynBin, agentBin, "fragile", []string{"timeout"})

	t.Logf("robust:  overall=%d coverage=%s events=%d failures=%v",
		robust.Report.Overall, robust.Coverage, robust.Events, robust.Failures)
	t.Logf("fragile: overall=%d coverage=%s events=%d failures=%v",
		fragile.Report.Overall, fragile.Coverage, fragile.Events, fragile.Failures)

	require.Positive(t, robust.Coverage.Injected,
		"no gremlin fired against the robust agent, so nothing was measured")
	require.Positive(t, fragile.Coverage.Injected,
		"no gremlin fired against the fragile agent, so nothing was measured")

	assert.Greater(t, robust.Report.Overall, fragile.Report.Overall,
		"an agent that reacts to a corrupted result must score higher than one that ignores it")
}

// Reproducibility at the report level: the same seed and the same agent must
// produce the same score, or a CI threshold would flap and get switched off.
func TestArenaCI_SameSeedSameScore(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes and compiles two binaries")
	}
	gremlynBin := buildGremlyn(t)
	agentBin := buildRefAgent(t)

	first := runCI(t, gremlynBin, agentBin, "fragile", []string{"corruption"})
	second := runCI(t, gremlynBin, agentBin, "fragile", []string{"corruption"})

	require.Positive(t, first.Coverage.Injected)
	assert.Equal(t, first.Report.Overall, second.Report.Overall,
		"the same seed and the same agent must produce the same score")
	assert.Equal(t, first.Coverage.Injected, second.Coverage.Injected,
		"the same seed must fire the same number of injections")
}

// An agent that never calls a tool must fail the scenario rather than scoring
// perfectly on an empty measurement.
func TestArenaCI_AgentThatNeverCallsATool_Fails(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	gremlynBin := buildGremlyn(t)

	cfg := &CIConfig{
		Agent: AgentConfig{
			// Reads the config path and exits, exactly like an agent that decided
			// not to use the tool.
			Command: []string{"sh", "-c", "cat " + PlaceholderMCPConfig + " >/dev/null; exit 0"},
			Timeout: 30 * time.Second,
		},
		MCPServer: MCPServerConfig{Name: "memory", Command: fakeMCPServer()},
	}
	sc := ScenarioConfig{Name: "no-tool-call", Gremlins: []string{"corruption"}, Seed: 1}

	res := runScenario(context.Background(), zerolog.Nop(), gremlynBin, cfg, sc, false)

	assert.False(t, res.Passed(), "a scenario that tested nothing must not pass")
	require.NotEmpty(t, res.Failures)
	assert.Contains(t, strings.Join(res.Failures, " "), "no")
}

// ── Output rendering ──

func TestWriteCIResult_JSONAndFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.json")

	result := CIResult{
		Passed: false,
		Scenarios: []ScenarioResult{{
			Name:     "s1",
			Report:   scoring.ResilienceReport{Overall: 40, Grade: scoring.GradeFromScore(40)},
			Coverage: coverage(2, 2, 0),
			Events:   2,
			Failures: []string{"overall resilience 40 is below the minimum 70"},
		}},
	}

	var buf strings.Builder
	require.NoError(t, writeCIResult(&buf, out, "json", result))

	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var round CIResult
	require.NoError(t, json.Unmarshal(data, &round))
	assert.False(t, round.Passed)
	require.Len(t, round.Scenarios, 1)
	assert.Equal(t, 40, round.Scenarios[0].Report.Overall)

	assert.Contains(t, buf.String(), `"passed": false`)
}

func TestWriteCIResult_Text(t *testing.T) {
	result := CIResult{
		Passed: false,
		Scenarios: []ScenarioResult{{
			Name:     "tool-failure",
			Report:   scoring.ResilienceReport{Overall: 40},
			Coverage: coverage(2, 1, 1),
			Failures: []string{"coverage incomplete"},
		}},
	}
	var buf strings.Builder
	require.NoError(t, writeCIResult(&buf, "", "text", result))

	got := buf.String()
	assert.Contains(t, got, "FAIL")
	assert.Contains(t, got, "tool-failure")
	assert.Contains(t, got, "coverage incomplete")
	assert.Contains(t, got, "Thresholds not met")
}

// ── helpers ──

// coverage builds a chaos.Coverage without repeating field names everywhere.
func coverage(injected, observed, unresolved int) chaos.Coverage {
	return chaos.Coverage{Injected: injected, Observed: observed, Unresolved: unresolved}
}

func indexOf(ss []string, want string) int {
	for i, s := range ss {
		if s == want {
			return i
		}
	}
	return -1
}

func TestSanitize(t *testing.T) {
	assert.Equal(t, "a-b_c-1", sanitize("a b_c/1"))
}

func TestTail(t *testing.T) {
	assert.Equal(t, "abc", tail("abc", 10))
	assert.Equal(t, "…c", tail("abc", 1))
}

func TestIntensityOf(t *testing.T) {
	assert.Equal(t, "medium", string(intensityOf(ScenarioConfig{})))
	assert.Equal(t, "high", string(intensityOf(ScenarioConfig{Intensity: "high"})))
}

func TestLoadCIConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "arena.yaml")
	body := fmt.Sprintf(`
agent:
  command: ["claude", "-p", "%s", "--mcp-config", "%s"]
  timeout: 2m
mcp_server:
  name: memory
  command: ["npx", "-y", "@modelcontextprotocol/server-memory"]
scenarios:
  - name: tool-failure
    gremlins: [corruption, latency]
    seed: 42
    intensity: high
    prompt: read the graph
thresholds:
  min_overall: 70
`, PlaceholderPrompt, PlaceholderMCPConfig)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cfg, err := loadCIConfig(path)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Minute, cfg.Agent.Timeout)
	assert.Equal(t, "memory", cfg.MCPServer.Name)
	require.Len(t, cfg.Scenarios, 1)
	assert.Equal(t, int64(42), cfg.Scenarios[0].Seed)
	assert.Equal(t, 70, cfg.Thresholds.MinOverall)
}
