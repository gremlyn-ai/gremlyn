package cli

import (
	"strings"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/stretchr/testify/assert"
)

func scenario(name string, gremlins []string, overall int, measured bool, injected int, failures ...string) ScenarioResult {
	return ScenarioResult{
		Name:     name,
		Gremlins: gremlins,
		Report: scoring.ResilienceReport{
			Overall: overall, Measured: measured, Grade: scoring.GradeFromScore(overall),
			Dimensions: map[scoring.Dimension]scoring.DimensionScore{
				scoring.DimensionCorruption: {Total: injected, Crashed: injected},
			},
		},
		Coverage: chaos.Coverage{Injected: injected, Observed: injected},
		Failures: failures,
	}
}

func TestReportTitle(t *testing.T) {
	pass := CIResult{Scenarios: []ScenarioResult{scenario("a", nil, 90, true, 1)}}
	assert.Equal(t, "All 1 scenarios passed", reportTitle(&pass, nil))

	cur := CIResult{Scenarios: []ScenarioResult{
		scenario("a", nil, 10, true, 1, "overall resilience 10 is below the minimum 50"),
		scenario("b", nil, 85, true, 1),
	}}
	base := CIResult{Scenarios: []ScenarioResult{scenario("a", nil, 90, true, 1), scenario("b", nil, 90, true, 1)}}
	assert.Equal(t, "1 of 2 scenarios below threshold · 1 regression vs base", reportTitle(&cur, &base),
		"a drops 80 (regression), b drops 5 (noise, not flagged)")
	assert.Equal(t, "No scenario ran", reportTitle(&CIResult{}, nil))
}

func TestRenderMarkdown(t *testing.T) {
	cur := CIResult{Scenarios: []ScenarioResult{
		scenario("corrupted-result", []string{"corruption"}, 10, true, 1, "overall resilience 10 is below the minimum 50"),
		scenario("injected-instructions", []string{"injection"}, 90, true, 1),
		scenario("no-tool-call", []string{"timeout"}, 0, false, 0, "nothing was measured"),
	}}
	base := CIResult{Scenarios: []ScenarioResult{
		scenario("corrupted-result", []string{"corruption"}, 90, true, 1),
		scenario("injected-instructions", []string{"injection"}, 90, true, 1),
	}}

	var b strings.Builder
	renderMarkdown(&b, &cur, &base)
	out := b.String()

	assert.Contains(t, out, "| ❌ | corrupted-result | corruption | **10** critical | 📉 -80 | 1/1 |")
	assert.Contains(t, out, "| ✅ | injected-instructions | injection | 90 excellent | = | 1/1 |")
	assert.Contains(t, out, "| ⚠️ | no-tool-call | timeout | not measured | new | 0/0 |",
		"an unmeasured scenario is a setup warning, never a score")
	assert.Contains(t, out, "validation of the result's shape", "the fix hint for the failing gremlin")
	assert.Contains(t, out, "Nothing was measured")
	assert.Contains(t, out, "gremlyn arena ci --scenario corrupted-result --scenario no-tool-call")
	assert.NotContains(t, out, "### ✅", "passing scenarios get no detail section")
}

func TestRenderMarkdown_NoBaseline(t *testing.T) {
	cur := CIResult{Scenarios: []ScenarioResult{scenario("a", []string{"timeout"}, 90, true, 2)}}
	var b strings.Builder
	renderMarkdown(&b, &cur, nil)
	out := b.String()
	assert.Contains(t, out, "| ✅ | a | timeout | 90 excellent |  | 2/2 |", "no delta column value without a baseline")
	assert.Contains(t, out, "No baseline was available")
	assert.NotContains(t, out, "**Fix it:**", "nothing to fix when everything passed")
}
