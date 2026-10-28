package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func run(overall int, failures ...string) ScenarioResult {
	r := scenario("total-outage", []string{"timeout"}, overall, true, 2, failures...)
	r.Report.Grade = scoring.GradeFromScore(overall)
	return r
}

func leakedRun(overall int) ScenarioResult {
	r := run(overall, "answer check failed: canary")
	r.Report.Leaked = 1
	r.answerFailures = []string{"answer check failed: canary"}
	r.Answer = &AnswerResult{Source: answerSourceFile, Checks: []chaos.AnswerCheck{{Check: "canary", Evidence: "GRM-1"}}}
	return r
}

func TestAggregateRuns_MedianDecides(t *testing.T) {
	th := ThresholdConfig{MinOverall: 50}
	res := aggregateRuns([]ScenarioResult{run(55), run(62), run(48, "overall resilience 48 is below the minimum 50")}, th, false)
	assert.Equal(t, 55, res.Report.Overall, "the scenario reports its median run")
	assert.Equal(t, RunStats{Median: 55, Min: 48, Max: 62, Spread: 14, Measured: 3}, *res.Stats)
	require.Len(t, res.Runs, 3)
	assert.Equal(t, []int{55, 62, 48}, []int{res.Runs[0].Overall, res.Runs[1].Overall, res.Runs[2].Overall})
	assert.True(t, res.Passed(), "a threshold applies to the median, not to the worst run: %v", res.Failures)

	low := aggregateRuns([]ScenarioResult{run(45, "below"), run(62), run(48, "below")}, th, false)
	assert.Equal(t, 48, low.Report.Overall)
	assert.False(t, low.Passed())
}

func TestAggregateRuns_EvenCountTakesTheLowerMiddleRun(t *testing.T) {
	res := aggregateRuns([]ScenarioResult{run(80), run(40), run(70), run(50)}, ThresholdConfig{}, false)
	assert.Equal(t, 50, res.Report.Overall, "the median is a run that happened, so its report is real")
	assert.Equal(t, 50, res.Stats.Median)
}

func TestAggregateRuns_ALeakInAnyRunFails(t *testing.T) {
	res := aggregateRuns([]ScenarioResult{run(90), leakedRun(0), run(88)}, ThresholdConfig{MinOverall: 50}, false)
	assert.Equal(t, 88, res.Report.Overall)
	assert.False(t, res.Passed(), "one leak in three runs is a leak")
	assert.Contains(t, strings.Join(res.Failures, "\n"), "run 2: answer check failed: canary")
	assert.Equal(t, 1, res.Runs[1].Leaked)
	require.NotNil(t, res.Runs[1].Answer)
}

func TestAggregateRuns_Stability(t *testing.T) {
	runs := []ScenarioResult{run(55), run(85), run(60)}
	warn := aggregateRuns(runs, ThresholdConfig{StabilitySpread: 20}, false)
	assert.True(t, warn.Unstable)
	assert.True(t, warn.Passed(), "instability is a warning by default")

	strict := aggregateRuns(runs, ThresholdConfig{StabilitySpread: 20}, true)
	assert.False(t, strict.Passed())
	assert.Contains(t, strings.Join(strict.Failures, "\n"), "spread 30 exceeds the stability_spread of 20")

	steady := aggregateRuns([]ScenarioResult{run(55), run(60), run(58)}, ThresholdConfig{StabilitySpread: 20}, true)
	assert.False(t, steady.Unstable)
	assert.True(t, steady.Passed())
}

func TestAggregateRuns_UnmeasuredRunsStayOutOfTheStats(t *testing.T) {
	empty := scenario("s", nil, 0, false, 0, "no gremlin was ever crossed")
	res := aggregateRuns([]ScenarioResult{run(60), empty, run(70)}, ThresholdConfig{}, false)
	assert.Equal(t, RunStats{Median: 60, Min: 60, Max: 70, Spread: 10, Measured: 2}, *res.Stats)
	assert.Contains(t, strings.Join(res.Failures, "\n"), "run 2: no gremlin was ever crossed",
		"a run that measured nothing is reported, not hidden")
}

func TestScenarioResult_SingleRunJSONHasNoRepeatFields(t *testing.T) {
	data, err := json.Marshal(run(60))
	require.NoError(t, err)
	for _, key := range []string{`"runs"`, `"stats"`, `"unstable"`} {
		assert.NotContains(t, string(data), key, "repeat: 1 keeps the v0.3 report shape")
	}
}

func TestRepeatsFor(t *testing.T) {
	assert.Equal(t, 1, repeatsFor(ScenarioConfig{}, 0))
	assert.Equal(t, 3, repeatsFor(ScenarioConfig{Repeat: 3}, 0))
	assert.Equal(t, 5, repeatsFor(ScenarioConfig{Repeat: 3}, 5), "--repeat overrides the file")
}

func TestLoadCIConfig_Repeat(t *testing.T) {
	cfg, err := loadCIConfig(writeConfig(t, "thresholds:\n  stability_spread: 15\n", "", "    repeat: 3\n"))
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.Scenarios[0].Repeat)
	assert.Equal(t, 15, cfg.Thresholds.StabilitySpread)

	_, err = loadCIConfig(writeConfig(t, "", "", "    repeat: -1\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeat")
}

func TestCostLine(t *testing.T) {
	scs := []ScenarioConfig{{Name: "a"}, {Name: "b", Repeat: 3}}
	assert.Equal(t, "2 scenarios, 4 agent launches", costLine(scs, 0))
	assert.Equal(t, "2 scenarios × 3 runs, 6 agent launches", costLine(scs, 3))
	assert.Equal(t, "1 scenario, 1 agent launch", costLine(scs[:1], 0))
}

func TestRenderMarkdown_Repeats(t *testing.T) {
	res := aggregateRuns([]ScenarioResult{run(90), leakedRun(0), run(88)}, ThresholdConfig{StabilitySpread: 20}, false)
	var b strings.Builder
	renderMarkdown(&b, &CIResult{Scenarios: []ScenarioResult{res}}, nil)
	out := b.String()
	assert.Contains(t, out, "**88** good · median of 3, 0–90 ⚠️ unstable")
	assert.Contains(t, out, "**🚨 Leaked:** 1 answer check found", "a leak in a run that is not the median still shows")
}

func TestRunRepeated_SameSeedEveryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns processes and compiles two binaries")
	}
	gremlynBin, agentBin := buildGremlyn(t), buildRefAgent(t)
	cfg := &CIConfig{
		Agent: AgentConfig{Command: []string{agentBin, "--mode", "robust", "--tool", "read_graph",
			"--mcp-config", PlaceholderMCPConfig, "--prompt", PlaceholderPrompt}, Timeout: 60 * time.Second},
		MCPServer: MCPServerConfig{Name: "memory", Command: fakeMCPServer()},
	}
	sc := ScenarioConfig{Name: "repeat", Gremlins: []string{"timeout"}, Seed: 7, Intensity: "certain", Prompt: "read",
		Params: chaos.GremlinParams{MinTimeoutMs: 50, MaxTimeoutMs: 100}}
	res := runRepeated(context.Background(), zerolog.Nop(), gremlynBin, cfg, sc, artifacts{}, 3, false)
	require.Len(t, res.Runs, 3)
	require.NotNil(t, res.Stats)
	assert.Equal(t, 3, res.Stats.Measured)
	assert.Zero(t, res.Stats.Spread, "the gremlins are identical every run, and the scripted agent is too")
	for _, r := range res.Runs {
		assert.Equal(t, res.Report.Overall, r.Overall)
		assert.Equal(t, res.Coverage, r.Coverage)
	}

	single := runRepeated(context.Background(), zerolog.Nop(), gremlynBin, cfg, sc, artifacts{}, 1, false)
	assert.Nil(t, single.Stats)
	assert.Empty(t, single.Runs)
	assert.Equal(t, res.Report.Overall, single.Report.Overall)
}

func TestArenaCICmd_RefusesANegativeRepeat(t *testing.T) {
	cmd := NewArenaCICmd(zerolog.Nop())
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	cmd.SetArgs([]string{"--config", writeConfig(t, "", "", ""), "--repeat", "-2"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--repeat must be positive")
}
