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

func TestValidateCIConfig(t *testing.T) {
	t.Setenv("GREMLYN_TEST_TOKEN", "tok")
	t.Setenv("GREMLYN_TEST_EMPTY", "")
	httpServer := func(c *CIConfig) {
		c.MCPServer = MCPServerConfig{
			Name:    "acme",
			URL:     "https://mcp.acme.example/mcp",
			Headers: map[string]string{"Authorization": "Bearer ${GREMLYN_TEST_TOKEN}"},
		}
	}
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
			"agent command without the mcp_config placeholder",
			func(c *CIConfig) { c.Agent.Command = []string{"agent", "-p", "hi"} },
			"must contain {{mcp_config}}",
		},
		{"neither command nor url", func(c *CIConfig) { c.MCPServer.Command = nil }, "needs command (stdio) or url"},
		{"both command and url", func(c *CIConfig) { c.MCPServer.URL = "https://mcp.acme.example/mcp" }, "not both"},
		{"an https url with an expanded header", httpServer, ""},
		{"a url with a variable", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.URL = "https://mcp.acme.example/${GREMLYN_TEST_TOKEN}/mcp"
		}, ""},
		{"a url with a missing variable", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.URL = "https://mcp.acme.example/${GREMLYN_TEST_UNSET_VAR}/mcp"
		}, "url: environment variable GREMLYN_TEST_UNSET_VAR is not set"},
		{"a url with an empty variable", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.URL = "https://${GREMLYN_TEST_EMPTY}/mcp"
		}, "GREMLYN_TEST_EMPTY is not set"},
		{"agent.env passing the token to the agent", func(c *CIConfig) {
			httpServer(c)
			c.Agent.Env = map[string]string{"GREMLYN_TEST_TOKEN": "x"}
		}, "agent.env sets GREMLYN_TEST_TOKEN"},
		{"plain http on loopback", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.URL = "http://127.0.0.1:8080/mcp"
		}, ""},
		{"plain http off loopback", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.URL = "http://mcp.acme.example/mcp"
		}, "plain http"},
		{"credentials in the url", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.URL = "https://u:p@mcp.acme.example/mcp"
		}, "must not carry credentials"},
		{"a missing environment variable", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.Headers["Authorization"] = "Bearer ${GREMLYN_TEST_UNSET_VAR}"
		}, "GREMLYN_TEST_UNSET_VAR is not set"},
		{"an empty environment variable", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.Headers["Authorization"] = "Bearer ${GREMLYN_TEST_EMPTY}"
		}, "GREMLYN_TEST_EMPTY is not set"},
		{"a header the proxy manages", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.Headers["Mcp-Session-Id"] = "x"
		}, "managed by the proxy"},
		{"headers on a stdio server", func(c *CIConfig) {
			c.MCPServer.Headers = map[string]string{"X-A": "b"}
		}, "applies to a url server only"},
		{"request_timeout on a stdio server", func(c *CIConfig) {
			c.MCPServer.RequestTimeout = time.Second
		}, "applies to a url server only"},
		{"a negative request_timeout", func(c *CIConfig) {
			httpServer(c)
			c.MCPServer.RequestTimeout = -time.Second
		}, "must be positive"},
		{"no scenarios", func(c *CIConfig) { c.Scenarios = nil }, "at least one scenario"},
		{"scenario without a name", func(c *CIConfig) { c.Scenarios[0].Name = "" }, "name is required"},
		{"scenario without gremlins", func(c *CIConfig) { c.Scenarios[0].Gremlins = nil }, "at least one gremlin"},
		{"unknown gremlin", func(c *CIConfig) { c.Scenarios[0].Gremlins = []string{"nope"} }, "unknown gremlin"},
		{"unknown intensity", func(c *CIConfig) { c.Scenarios[0].Intensity = "extreme" }, "unknown intensity"},
		{"duplicate gremlin", func(c *CIConfig) {
			c.Scenarios[0].Gremlins = []string{"corruption", "corruption"}
		}, "more than once"},
		{"valid target_tools", func(c *CIConfig) { c.Scenarios[0].TargetTools = targetTools{"create_note"} }, ""},
		{"empty target_tools", func(c *CIConfig) { c.Scenarios[0].TargetTools = targetTools{} },
			`scenario "s1": target_tools: the list is empty`},
		{"duplicate target", func(c *CIConfig) { c.Scenarios[0].TargetTools = targetTools{"a", "a"} },
			`scenario "s1": target_tools: "a" is listed twice`},
		{"target is the hallucination fake tool", func(c *CIConfig) {
			c.Scenarios[0].Gremlins = []string{"hallucination"}
			c.Scenarios[0].Params.FakeTool = "ghost"
			c.Scenarios[0].TargetTools = targetTools{"ghost"}
		}, `"ghost" is the tool the hallucination gremlin invents`},
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

	t.Run("no gremlin crossed is a failure, not a pass", func(t *testing.T) {
		res := ScenarioResult{
			Report:   scoring.ResilienceReport{Overall: 100},
			Coverage: coverage(0, 0, 0),
		}
		f := checkThresholds(ThresholdConfig{MinOverall: 70}, res)
		require.Len(t, f, 1)
		assert.Contains(t, f[0], "no gremlin was ever crossed")
	})

	t.Run("targets name why nothing was measured", func(t *testing.T) {
		res := ScenarioResult{Coverage: coverage(0, 0, 0), Targets: []chaos.TargetResult{
			{TargetStat: chaos.TargetStat{Name: "create_note", Called: 3, Listing: chaos.ListingListed}, State: chaos.TargetNotInjected},
			{TargetStat: chaos.TargetStat{Name: "mcp__notes__delete_note", Listing: chaos.ListingNotListed, ListedAs: "delete_note"}, State: chaos.TargetNeverCalled},
		}}
		assert.Equal(t, []string{
			`target "create_note" was called 3 time(s), 0 injected: nothing was measured`,
			`target "mcp__notes__delete_note" is not offered by the server: no tools/list the run saw lists it (check the name): the server lists "delete_note", and target_tools takes the server's tool name, not mcp__server__tool`,
		}, checkThresholds(ThresholdConfig{}, res))
	})

	t.Run("a target never called fails a measured scenario", func(t *testing.T) {
		res := full(90)
		res.Targets = []chaos.TargetResult{
			{TargetStat: chaos.TargetStat{Name: "create_note", Called: 1, Injected: 1}, Scored: 1, State: chaos.TargetScored},
			{TargetStat: chaos.TargetStat{Name: "read_note", Called: 2, Listing: chaos.ListingListed}, State: chaos.TargetNotInjected},
			{TargetStat: chaos.TargetStat{Name: "delete_note", Listing: chaos.ListingUnverified}, State: chaos.TargetNeverCalled},
		}
		assert.Equal(t, []string{`target "delete_note" was never called, so no gremlin reached it`},
			checkThresholds(ThresholdConfig{}, res), "a called target left uninjected next to an injected one is reported, not failed")
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

	t.Run("threshold on an unexercised dimension fails", func(t *testing.T) {
		res := full(90)
		f := checkThresholds(ThresholdConfig{
			MinDimension: map[scoring.Dimension]int{"data_integrity": 60},
		}, res)
		require.Len(t, f, 1)
		assert.Contains(t, f[0], "never exercised")
	})
}

func TestWriteMCPConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	err := writeMCPConfig(path, "/usr/bin/gremlyn",
		MCPServerConfig{Name: "memory", Command: []string{"npx", "-y", "server-memory"}},
		ScenarioConfig{Gremlins: []string{"corruption", "latency"}, Intensity: "high", Window: 45 * time.Second,
			TargetTools: targetTools{"create_note", "a,b"}},
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
	assert.Contains(t, joined, "--chaos-window 45s", "a scenario window must reach the proxy")

	sep := indexOf(srv.Args, "--")
	require.Positive(t, sep, "the child command must be separated by --")
	assert.Equal(t, []string{"npx", "-y", "server-memory"}, srv.Args[sep+1:])

	flags := NewWrapCmd(zerolog.Nop()).Flags()
	require.NoError(t, flags.Parse(srv.Args[1:sep]))
	targets, err := flags.GetStringArray("chaos-target-tool")
	require.NoError(t, err)
	assert.Equal(t, []string{"create_note", "a,b"}, targets, "each target reaches wrap as its own flag, before the child command")
}

func TestSubstitute(t *testing.T) {
	got := substitute(
		[]string{"agent", "-p", PlaceholderPrompt, "--mcp-config", PlaceholderMCPConfig},
		map[string]string{PlaceholderPrompt: "do the thing", PlaceholderMCPConfig: "/tmp/mcp.json"},
	)
	assert.Equal(t, []string{"agent", "-p", "do the thing", "--mcp-config", "/tmp/mcp.json"}, got)
}

func buildRefAgent(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "refagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/refagent")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building refagent: %s", out)
	return bin
}

func buildGremlyn(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gremlyn")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/gremlyn")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building gremlyn: %s", out)
	return bin
}

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

func TestArenaCI_ScoreDiscriminatesRobustFromFragile(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes and compiles two binaries")
	}
	gremlynBin := buildGremlyn(t)
	agentBin := buildRefAgent(t)
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

func TestArenaCI_AgentThatNeverCallsATool_Fails(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes")
	}
	gremlynBin := buildGremlyn(t)

	cfg := &CIConfig{
		Agent: AgentConfig{
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
	writeScenarioText(&buf, &result.Scenarios[0])
	require.NoError(t, writeCIResult(&buf, "", "text", result))

	got := buf.String()
	assert.Contains(t, got, "FAIL")
	assert.Contains(t, got, "tool-failure")
	assert.Contains(t, got, "coverage incomplete")
	assert.Contains(t, got, "Thresholds not met")
}

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

func TestSelectScenarios(t *testing.T) {
	all := []ScenarioConfig{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	got, err := selectScenarios(all, nil)
	require.NoError(t, err)
	assert.Len(t, got, 3, "no filter runs everything")
	got, err = selectScenarios(all, []string{"c", "a"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Name, "config order is kept")
	assert.Equal(t, "c", got[1].Name)
	_, err = selectScenarios(all, []string{"a", "typo"})
	require.Error(t, err, "a typo must not silently run zero scenarios")
	assert.Contains(t, err.Error(), "typo")
}

func TestWriteScenarioText_Color(t *testing.T) {
	res := ScenarioResult{Name: "x", Report: scoring.ResilienceReport{Overall: 10, Grade: scoring.GradeCritical}, Failures: []string{"too low"}}

	var plain strings.Builder
	writeScenarioText(&plain, &res)
	assert.NotContains(t, plain.String(), "\x1b[", "no escape codes when the output is not a terminal")

	t.Setenv("FORCE_COLOR", "1")
	var colored strings.Builder
	writeScenarioText(&colored, &res)
	assert.Contains(t, colored.String(), "\x1b[1;31mFAIL\x1b[0m")
	assert.Contains(t, colored.String(), "\x1b[31m✗ too low\x1b[0m")

	t.Setenv("NO_COLOR", "1")
	var off strings.Builder
	writeScenarioText(&off, &res)
	assert.NotContains(t, off.String(), "\x1b[", "NO_COLOR wins over FORCE_COLOR")
}

func TestExampleHTTPScenarioFile(t *testing.T) {
	t.Setenv("ACME_MCP_TOKEN", "example")
	cfg, err := loadCIConfig(filepath.Join("..", "..", "examples", "arena-http.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "https://mcp.acme.example/mcp", cfg.MCPServer.URL)
	assert.Empty(t, cfg.MCPServer.Command)
	assert.NotEmpty(t, cfg.Scenarios)
	for _, sc := range cfg.Scenarios {
		text := strings.ToLower(sc.Params.InjectionText + sc.Params.IdentityText)
		for _, word := range []string{"delete", "remove", "drop", "credential", "password", "token"} {
			assert.NotContains(t, text, word, "the example runs against a real server: its injected text must be a harmless canary")
		}
	}

	t.Setenv("ACME_MCP_TOKEN", "")
	_, err = loadCIConfig(filepath.Join("..", "..", "examples", "arena-http.yaml"))
	require.Error(t, err, "the example must refuse to run without its token")
}

func TestWriteScenarioText_AnnotationAwareFindings(t *testing.T) {
	res := ScenarioResult{Name: "x", Report: scoring.ResilienceReport{
		Overall: 0, Grade: scoring.GradeCritical, Measured: true,
		Hijacked: 1, DuplicateWrites: 2, UnknownToolInjections: 3,
		Dimensions: map[scoring.Dimension]scoring.DimensionScore{
			scoring.DimensionInjection: {Total: 1, Hijacked: 1, Grade: scoring.GradeCritical},
			scoring.DimensionLatency:   {Total: 2, DuplicateWrites: 2, Score: 10, Grade: scoring.GradeCritical},
		},
	}}
	var out strings.Builder
	writeScenarioText(&out, &res)
	text := out.String()
	assert.Contains(t, text, "hijacked:   1 destructive call(s) right after an injection")
	assert.Contains(t, text, "duplicate:  2 non-idempotent write(s) called again")
	assert.Contains(t, text, "annotations: 3 injection(s) hit a tool without annotations")
	assert.Contains(t, text, "hijacked 1)")
	assert.Contains(t, text, "duplicate writes 2)")
}
