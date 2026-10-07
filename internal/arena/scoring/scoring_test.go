package scoring

import (
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
)

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

func TestScore_EmptyEvents(t *testing.T) {
	report := Score(nil, nil)
	assert.Equal(t, 0, report.Overall)
	assert.Equal(t, GradeNoData, report.Grade)
	assert.False(t, report.Measured, "no event was scored, so nothing was measured")
}

func TestScore_MeasuredDistinguishesNoDataFromTotalFailure(t *testing.T) {
	empty := Score(nil, nil)

	failed := Score([]models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "latency", Outcome: models.OutcomeCrashed, Score: 0},
	}, nil)

	assert.False(t, empty.Measured)
	assert.True(t, failed.Measured, "events that mapped to a dimension were measured")

	assert.Equal(t, GradeNoData, empty.Grade)
	assert.Equal(t, GradeCritical, failed.Grade)
	assert.NotEqual(t, empty.Grade, failed.Grade,
		"a session that measured nothing must not look like one that failed everything")
}

func TestScore_ReportsUnmappedGremlins(t *testing.T) {
	report := Score([]models.ArenaEvent{
		{GremlinType: "brand_new_gremlin", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "another_unmapped", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "brand_new_gremlin", Outcome: models.OutcomeSurvived, Score: 90},
	}, nil)
	assert.Equal(t, []string{"another_unmapped", "brand_new_gremlin"}, report.UnmappedGremlins)
	assert.False(t, report.Measured, "unmappable events contribute nothing to the score")
	assert.Equal(t, GradeNoData, report.Grade)
}

func TestScore_EveryKnownGremlinMapsToADimension(t *testing.T) {
	known := []string{
		"hallucination", "latency", "corruption", "loop",
		"injection", "identity", "overflow", "timeout",
	}
	for _, name := range known {
		t.Run(name, func(t *testing.T) {
			report := Score([]models.ArenaEvent{
				{GremlinType: name, Outcome: models.OutcomeSurvived, Score: 90},
			}, nil)
			assert.Empty(t, report.UnmappedGremlins,
				"gremlin %q has no entry in GremlinToDimension, so its events are discarded", name)
			assert.True(t, report.Measured)
		})
	}
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
	assert.Equal(t, 50, ds.Score)
}

func TestScore_CustomWeights(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeCrashed, Score: 0},
	}

	weights := map[Dimension]float64{
		DimensionHallucination: 0.9,
		DimensionLatency:       0.1,
		DimensionCorruption:    0.0,
		DimensionLoop:          0.0,
		DimensionInjection:     0.0,
	}

	report := Score(events, weights)
	assert.Equal(t, 90, report.Overall)
}

func TestScore_UnknownGremlinTypeIgnored(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "unknown_gremlin", Outcome: models.OutcomeSurvived, Score: 100},
	}

	report := Score(events, nil)
	assert.Equal(t, 0, report.Overall)
}

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
