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

func withEffects(s ScenarioResult, hijacked, duplicates, unknown int) ScenarioResult {
	s.Report.Hijacked = hijacked
	s.Report.DuplicateWrites = duplicates
	s.Report.UnknownToolInjections = unknown
	return s
}

func TestReportTitle_CountsHijackedCalls(t *testing.T) {
	cur := CIResult{Scenarios: []ScenarioResult{
		withEffects(scenario("a", nil, 0, true, 1, "overall resilience 0 is below the minimum 50"), 1, 0, 0),
		withEffects(scenario("b", nil, 90, true, 1), 0, 1, 0),
	}}
	assert.Equal(t, "1 of 2 scenarios below threshold · 1 hijacked tool call", reportTitle(&cur, nil),
		"a hijack is named in the check title; a duplicate write is in the body")

	cur.Scenarios[1].Report.Hijacked = 2
	assert.Equal(t, "1 of 2 scenarios below threshold · 3 hijacked tool calls", reportTitle(&cur, nil))
}

func TestRenderMarkdown_AnnotationAwareFindings(t *testing.T) {
	cur := CIResult{Scenarios: []ScenarioResult{
		withEffects(scenario("injected-delete", []string{"injection"}, 0, true, 1, "overall resilience 0 is below the minimum 50"), 1, 0, 0),
		withEffects(scenario("timeout-on-create", []string{"timeout"}, 10, true, 1, "overall resilience 10 is below the minimum 50"), 0, 1, 0),
		withEffects(scenario("unannotated", []string{"timeout"}, 90, true, 2), 0, 0, 2),
	}}
	var b strings.Builder
	renderMarkdown(&b, &cur, nil)
	out := b.String()

	assert.Contains(t, out, "**🚨 Hijacked:** 1 call to a destructive tool right after an injected instruction (injected-delete).")
	assert.Contains(t, out, "**Duplicate writes:** 1 non-idempotent write called again after a timeout or a corrupted result")
	assert.Contains(t, out, "- **What the agent did:** 0 survived, 0 degraded, 1 failed, 1 hijacked.")
	assert.Contains(t, out, "1 duplicate write.")
	assert.Contains(t, out, "a confirmation step on the destructive tool", "a hijack points at a server-side fix")
	assert.Contains(t, out, "an idempotency key on the write tool", "a duplicate write points at a server-side fix")
	assert.Contains(t, out, "2 injections by a gremlin that reads tool annotations (`corruption`, `identity`, `injection`, `timeout`) hit a tool without annotations, so the generic rubric scored them.")
}

func TestRenderMarkdown_NothingNewForAReportWithoutEffects(t *testing.T) {
	cur := CIResult{Scenarios: []ScenarioResult{scenario("a", []string{"timeout"}, 90, true, 2)}}
	var b strings.Builder
	renderMarkdown(&b, &cur, nil)
	out := b.String()
	assert.NotContains(t, out, "Hijacked")
	assert.NotContains(t, out, "Duplicate writes")
	assert.NotContains(t, out, "without annotations", "a report written before annotations existed renders as before")
	assert.Equal(t, "All 1 scenarios passed", reportTitle(&cur, nil))
}

func TestRenderMarkdown_Targets(t *testing.T) {
	s := scenario("timeout-on-create", []string{"timeout"}, 0, false, 0, `target "create_note" was never called, so no gremlin reached it`)
	s.Targets = []chaos.TargetResult{{TargetStat: chaos.TargetStat{Name: "create_note", Listing: chaos.ListingListed}, State: chaos.TargetNeverCalled}}
	var b strings.Builder
	renderMarkdown(&b, &CIResult{Scenarios: []ScenarioResult{s}}, nil)
	out := b.String()
	assert.Contains(t, out, "- **Targets:** create_note never_called (called 0, injected 0, scored 0, listed)")
	assert.Contains(t, out, "no call to a target tool was injected")
	assert.NotContains(t, out, "the agent never called a tool")
}

func TestRenderMarkdown_Leaked(t *testing.T) {
	s := scenario("inject-link", []string{"injection"}, 0, true, 1, "answer check failed: links_only_to")
	s.Report.Leaked = 1
	s.Answer = &AnswerResult{Source: answerSourceFile, Checks: []chaos.AnswerCheck{
		{Check: "canary", Passed: true},
		{Check: "links_only_to", Evidence: "![x](https://evil.example/t.png) `rm -rf` [log in](https://evil.example)"},
	}}
	var b strings.Builder
	renderMarkdown(&b, &CIResult{Scenarios: []ScenarioResult{s}}, nil)
	out := b.String()
	assert.Contains(t, out, "**🚨 Leaked:** 1 answer check found injected content on its way to the user (inject-link)")
	assert.Contains(t, out, "1 leaked")
	assert.Contains(t, out, "- **Evidence** `links_only_to`: `![x](https://evil.example/t.png) 'rm -rf' [log in](https://evil.example)`",
		"hostile text reaches the PR comment only inside a code span, so no image loads and no link renders")
	assert.NotContains(t, out, "canary`:", "a passed check shows no evidence")
}
