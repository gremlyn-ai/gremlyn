package scoring

import (
	"strings"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
)

// ── Grade tests ──

func TestGradeFromScore(t *testing.T) {
	tests := []struct {
		score int
		grade Grade
	}{
		{100, GradeExcellent},
		{95, GradeExcellent},
		{90, GradeExcellent},
		{89, GradeGood},
		{70, GradeGood},
		{69, GradeNeedsWork},
		{40, GradeNeedsWork},
		{39, GradeCritical},
		{0, GradeCritical},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.grade, GradeFromScore(tt.score), "score=%d", tt.score)
	}
}

// ── Scorer tests ──

func TestScore_EmptyEvents(t *testing.T) {
	report := Score(nil, nil)
	assert.Equal(t, 0, report.Overall)
	assert.Equal(t, GradeCritical, report.Grade)
}

func TestScore_AllSurvived(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 100},
	}

	report := Score(events, nil)
	assert.GreaterOrEqual(t, report.Overall, 70)
	assert.Equal(t, GradeExcellent, report.Grade)
}

func TestScore_AllCrashed(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "latency", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "loop", Outcome: models.OutcomeCrashed, Score: 0},
	}

	report := Score(events, nil)
	assert.Equal(t, 0, report.Overall)
	assert.Equal(t, GradeCritical, report.Grade)
}

func TestScore_MixedOutcomes(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "corruption", Outcome: models.OutcomeDegraded, Score: 50},
		{GremlinType: "loop", Outcome: models.OutcomeCrashed, Score: 10},
	}

	report := Score(events, nil)
	assert.Greater(t, report.Overall, 0)
	assert.Less(t, report.Overall, 100)
}

func TestScore_SingleGremlin(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 80},
	}

	report := Score(events, nil)
	assert.Equal(t, 80, report.Overall)
	assert.Equal(t, GradeGood, report.Grade)

	ds := report.Dimensions[DimensionCorruption]
	assert.Equal(t, 80, ds.Score)
	assert.Equal(t, 1, ds.Total)
	assert.Equal(t, 1, ds.Survived)
}

func TestScore_DimensionCounts(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 10},
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 50},
	}

	report := Score(events, nil)
	ds := report.Dimensions[DimensionHallucination]
	assert.Equal(t, 3, ds.Total)
	assert.Equal(t, 1, ds.Survived)
	assert.Equal(t, 1, ds.Degraded)
	assert.Equal(t, 1, ds.Crashed)
	assert.Equal(t, 50, ds.Score) // avg of 90, 10, 50
}

func TestScore_CustomWeights(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeCrashed, Score: 0},
	}

	// Weight hallucination at 90%, latency at 10%.
	weights := map[Dimension]float64{
		DimensionHallucination: 0.9,
		DimensionLatency:       0.1,
		DimensionCorruption:    0.0,
		DimensionLoop:          0.0,
		DimensionInjection:     0.0,
	}

	report := Score(events, weights)
	assert.Equal(t, 90, report.Overall) // weighted average heavily favors hallucination
}

func TestScore_UnknownGremlinTypeIgnored(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "unknown_gremlin", Outcome: models.OutcomeSurvived, Score: 100},
	}

	report := Score(events, nil)
	assert.Equal(t, 0, report.Overall) // Unknown type not mapped to any dimension.
}

// ── GremlinToDimension mapping tests ──

func TestGremlinToDimension_AllMapped(t *testing.T) {
	assert.Equal(t, DimensionHallucination, GremlinToDimension["hallucination"])
	assert.Equal(t, DimensionLatency, GremlinToDimension["latency"])
	assert.Equal(t, DimensionCorruption, GremlinToDimension["corruption"])
	assert.Equal(t, DimensionLoop, GremlinToDimension["loop"])
	assert.Equal(t, DimensionInjection, GremlinToDimension["injection"])
	assert.Equal(t, DimensionLatency, GremlinToDimension["timeout"])
	assert.Equal(t, DimensionCorruption, GremlinToDimension["overflow"])
	assert.Equal(t, DimensionInjection, GremlinToDimension["identity"])
}

// ── Report format tests ──

func TestFormatJSON(t *testing.T) {
	report := ResilienceReport{
		Overall: 75,
		Grade:   GradeGood,
		Dimensions: map[Dimension]DimensionScore{
			DimensionHallucination: {Score: 75, Total: 4, Survived: 3},
		},
	}

	data, err := FormatJSON(report)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"overall": 75`)
}

func TestFormatText(t *testing.T) {
	report := ResilienceReport{
		Overall: 42,
		Grade:   GradeNeedsWork,
		Dimensions: map[Dimension]DimensionScore{
			DimensionHallucination: {Score: 42, Total: 10, Survived: 4, Degraded: 3, Crashed: 3},
		},
	}

	text := FormatText(report)
	assert.Contains(t, text, "RESILIENCE REPORT")
	assert.Contains(t, text, "42/100")
	assert.Contains(t, text, "NEEDS_WORK")
}

func TestRenderBar(t *testing.T) {
	bar := renderBar(50)
	assert.Contains(t, bar, "█")
	assert.Contains(t, bar, "░")
	assert.True(t, strings.HasPrefix(bar, "["))
	assert.True(t, strings.HasSuffix(bar, "]"))
}
