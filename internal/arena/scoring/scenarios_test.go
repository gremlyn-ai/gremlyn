package scoring

import (
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Production scenario: CRM agent with mixed resilience
// ---------------------------------------------------------------------------

func TestProductionScoring_CRMAgentMixedResilience(t *testing.T) {
	// Simulates a CRM agent tested with all gremlin types.
	// Hallucination: good (7 survived, 2 degraded, 1 crashed out of 10)
	// Latency: excellent (5/5 survived)
	// Corruption: weak (2 survived, 2 degraded, 1 crashed out of 5)
	// Loop: decent (2 survived, 1 degraded out of 3)
	events := []models.ArenaEvent{
		// Hallucination: 7 survived
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 85},
		// Hallucination: 2 degraded
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 50},
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 45},
		// Hallucination: 1 crashed
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 0},

		// Latency: 5 survived (agent handles timeouts well)
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 85},

		// Corruption: 2 survived, 2 degraded, 1 crashed
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 75},
		{GremlinType: "corruption", Outcome: models.OutcomeDegraded, Score: 40},
		{GremlinType: "corruption", Outcome: models.OutcomeDegraded, Score: 35},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},

		// Loop: 2 survived, 1 degraded
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "loop", Outcome: models.OutcomeDegraded, Score: 50},
	}

	report := Score(events, nil)

	// Overall should be moderate (60-80 range, weighted avg of dimensions).
	assert.GreaterOrEqual(t, report.Overall, 55)
	assert.LessOrEqual(t, report.Overall, 85)
	assert.Equal(t, GradeGood, report.Grade)

	// Hallucination dimension.
	hall := report.Dimensions[DimensionHallucination]
	assert.Equal(t, 10, hall.Total)
	assert.Equal(t, 7, hall.Survived)
	assert.Equal(t, 2, hall.Degraded)
	assert.Equal(t, 1, hall.Crashed)
	assert.Equal(t, GradeGood, hall.Grade) // avg ~72

	// Latency dimension.
	lat := report.Dimensions[DimensionLatency]
	assert.Equal(t, 5, lat.Total)
	assert.Equal(t, 5, lat.Survived)
	assert.Equal(t, 0, lat.Crashed)
	assert.Equal(t, GradeExcellent, lat.Grade) // avg 94

	// Corruption dimension.
	corr := report.Dimensions[DimensionCorruption]
	assert.Equal(t, 5, corr.Total)
	assert.Equal(t, 2, corr.Survived)
	assert.Equal(t, 2, corr.Degraded)
	assert.Equal(t, 1, corr.Crashed)
	assert.Equal(t, GradeNeedsWork, corr.Grade) // avg 46

	// Loop dimension.
	loop := report.Dimensions[DimensionLoop]
	assert.Equal(t, 3, loop.Total)
	assert.Equal(t, 2, loop.Survived)
	assert.Equal(t, 1, loop.Degraded)
	assert.Equal(t, 0, loop.Crashed)
	assert.Equal(t, GradeGood, loop.Grade) // avg 75

	// Injection dimension should be empty (no injection events).
	inj := report.Dimensions[DimensionInjection]
	assert.Equal(t, 0, inj.Total)
}

// ---------------------------------------------------------------------------
// Production scenario: Well-hardened agent — excellent grade
// ---------------------------------------------------------------------------

func TestProductionScoring_WellHardenedAgent(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "injection", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "injection", Outcome: models.OutcomeSurvived, Score: 95},
	}

	report := Score(events, nil)
	assert.GreaterOrEqual(t, report.Overall, 90)
	assert.Equal(t, GradeExcellent, report.Grade)

	// All dimensions should be excellent.
	for _, dim := range AllDimensions {
		ds := report.Dimensions[dim]
		if ds.Total > 0 {
			assert.Equal(t, GradeExcellent, ds.Grade, "dimension %s should be excellent", dim)
			assert.Equal(t, 0, ds.Crashed, "dimension %s should have zero crashes", dim)
		}
	}
}

// ---------------------------------------------------------------------------
// Production scenario: New unprotected agent — critical grade
// ---------------------------------------------------------------------------

func TestProductionScoring_UnprotectedAgent(t *testing.T) {
	// Agent with no resilience — crashes on everything.
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 5},
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "latency", Outcome: models.OutcomeCrashed, Score: 10},
		{GremlinType: "latency", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 5},
		{GremlinType: "loop", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "injection", Outcome: models.OutcomeCrashed, Score: 0},
	}

	report := Score(events, nil)
	assert.LessOrEqual(t, report.Overall, 10)
	assert.Equal(t, GradeCritical, report.Grade)

	// Every dimension should be critical.
	for _, dim := range AllDimensions {
		ds := report.Dimensions[dim]
		if ds.Total > 0 {
			assert.Equal(t, GradeCritical, ds.Grade, "dimension %s should be critical", dim)
			assert.Equal(t, 0, ds.Survived, "dimension %s should have zero survivals", dim)
		}
	}
}

// ---------------------------------------------------------------------------
// Production scenario: Database agent — high corruption vulnerability
// ---------------------------------------------------------------------------

func TestProductionScoring_DatabaseAgentCorruptionVulnerable(t *testing.T) {
	// DB agent: great at latency handling but terrible at corruption recovery.
	events := []models.ArenaEvent{
		// Latency: all survived (has proper timeout/retry)
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "timeout", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "timeout", Outcome: models.OutcomeSurvived, Score: 90},

		// Corruption: mostly crashes (no input validation on DB results)
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 5},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "corruption", Outcome: models.OutcomeDegraded, Score: 30},
		{GremlinType: "overflow", Outcome: models.OutcomeCrashed, Score: 0},

		// Hallucination: decent
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 50},
	}

	report := Score(events, nil)

	// Latency should be excellent (timeout maps to latency dimension too).
	lat := report.Dimensions[DimensionLatency]
	assert.Equal(t, 5, lat.Total) // 3 latency + 2 timeout
	assert.Equal(t, GradeExcellent, lat.Grade)

	// Corruption should be critical (corruption + overflow both map here).
	corr := report.Dimensions[DimensionCorruption]
	assert.Equal(t, 5, corr.Total) // 4 corruption + 1 overflow
	assert.Equal(t, GradeCritical, corr.Grade)

	// Overall should be dragged down by corruption.
	assert.Less(t, report.Overall, 80)
}

// ---------------------------------------------------------------------------
// Production scenario: Custom weights prioritize injection defense
// ---------------------------------------------------------------------------

func TestProductionScoring_CustomWeightsPrioritizeInjection(t *testing.T) {
	events := []models.ArenaEvent{
		// Hallucination: excellent
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 95},

		// Injection: terrible
		{GremlinType: "injection", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "injection", Outcome: models.OutcomeCrashed, Score: 10},
		{GremlinType: "identity", Outcome: models.OutcomeCrashed, Score: 5},
	}

	// Default weights: equal 20% each.
	defaultReport := Score(events, nil)

	// Custom weights: injection is 60%, hallucination is 40%.
	customWeights := map[Dimension]float64{
		DimensionHallucination: 0.4,
		DimensionLatency:       0.0,
		DimensionCorruption:    0.0,
		DimensionLoop:          0.0,
		DimensionInjection:     0.6,
	}
	customReport := Score(events, customWeights)

	// Custom report should be much lower because injection (which is terrible)
	// has 60% weight instead of 20%.
	assert.Less(t, customReport.Overall, defaultReport.Overall)
	assert.LessOrEqual(t, customReport.Overall, 45)

	// Injection dimension should be critical regardless of weights.
	inj := customReport.Dimensions[DimensionInjection]
	assert.Equal(t, 3, inj.Total) // 2 injection + 1 identity
	assert.Equal(t, GradeCritical, inj.Grade)
}

// ---------------------------------------------------------------------------
// Production scenario: Full report format is readable
// ---------------------------------------------------------------------------

func TestProductionScoring_FullReportFormat(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 50},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 10},
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "injection", Outcome: models.OutcomeSurvived, Score: 70},
	}

	report := Score(events, nil)

	// JSON format.
	jsonBytes, err := FormatJSON(report)
	require.NoError(t, err)
	jsonStr := string(jsonBytes)
	assert.Contains(t, jsonStr, `"overall"`)
	assert.Contains(t, jsonStr, `"grade"`)
	assert.Contains(t, jsonStr, `"hallucination_tolerance"`)
	assert.Contains(t, jsonStr, `"latency_handling"`)
	assert.Contains(t, jsonStr, `"corruption_recovery"`)
	assert.Contains(t, jsonStr, `"loop_prevention"`)
	assert.Contains(t, jsonStr, `"injection_defense"`)

	// Text format.
	text := FormatText(report)
	assert.Contains(t, text, "RESILIENCE REPORT")
	assert.Contains(t, text, "/100")
	// Should contain bar characters.
	assert.Contains(t, text, "█")
}
