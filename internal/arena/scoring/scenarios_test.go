package scoring

import (
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
)

func TestProductionScoring_CRMAgentMixedResilience(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 50},
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 45},
		{GremlinType: "hallucination", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "corruption", Outcome: models.OutcomeSurvived, Score: 75},
		{GremlinType: "corruption", Outcome: models.OutcomeDegraded, Score: 40},
		{GremlinType: "corruption", Outcome: models.OutcomeDegraded, Score: 35},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "loop", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "loop", Outcome: models.OutcomeDegraded, Score: 50},
	}

	report := Score(events, nil)

	assert.GreaterOrEqual(t, report.Overall, 55)
	assert.LessOrEqual(t, report.Overall, 85)
	assert.Equal(t, GradeGood, report.Grade)

	hall := report.Dimensions[DimensionHallucination]
	assert.Equal(t, 10, hall.Total)
	assert.Equal(t, 7, hall.Survived)
	assert.Equal(t, 2, hall.Degraded)
	assert.Equal(t, 1, hall.Crashed)
	assert.Equal(t, GradeGood, hall.Grade)

	lat := report.Dimensions[DimensionLatency]
	assert.Equal(t, 5, lat.Total)
	assert.Equal(t, 5, lat.Survived)
	assert.Equal(t, 0, lat.Crashed)
	assert.Equal(t, GradeExcellent, lat.Grade)

	corr := report.Dimensions[DimensionCorruption]
	assert.Equal(t, 5, corr.Total)
	assert.Equal(t, 2, corr.Survived)
	assert.Equal(t, 2, corr.Degraded)
	assert.Equal(t, 1, corr.Crashed)
	assert.Equal(t, GradeNeedsWork, corr.Grade)

	loop := report.Dimensions[DimensionLoop]
	assert.Equal(t, 3, loop.Total)
	assert.Equal(t, 2, loop.Survived)
	assert.Equal(t, 1, loop.Degraded)
	assert.Equal(t, 0, loop.Crashed)
	assert.Equal(t, GradeGood, loop.Grade)

	inj := report.Dimensions[DimensionInjection]
	assert.Equal(t, 0, inj.Total)
}

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

	for _, dim := range AllDimensions {
		ds := report.Dimensions[dim]
		if ds.Total > 0 {
			assert.Equal(t, GradeExcellent, ds.Grade, "dimension %s should be excellent", dim)
			assert.Equal(t, 0, ds.Crashed, "dimension %s should have zero crashes", dim)
		}
	}
}

func TestProductionScoring_UnprotectedAgent(t *testing.T) {
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

	for _, dim := range AllDimensions {
		ds := report.Dimensions[dim]
		if ds.Total > 0 {
			assert.Equal(t, GradeCritical, ds.Grade, "dimension %s should be critical", dim)
			assert.Equal(t, 0, ds.Survived, "dimension %s should have zero survivals", dim)
		}
	}
}

func TestProductionScoring_DatabaseAgentCorruptionVulnerable(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "latency", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "timeout", Outcome: models.OutcomeSurvived, Score: 85},
		{GremlinType: "timeout", Outcome: models.OutcomeSurvived, Score: 90},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 5},
		{GremlinType: "corruption", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "corruption", Outcome: models.OutcomeDegraded, Score: 30},
		{GremlinType: "overflow", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 80},
		{GremlinType: "hallucination", Outcome: models.OutcomeDegraded, Score: 50},
	}

	report := Score(events, nil)

	lat := report.Dimensions[DimensionLatency]
	assert.Equal(t, 5, lat.Total)
	assert.Equal(t, GradeExcellent, lat.Grade)

	corr := report.Dimensions[DimensionCorruption]
	assert.Equal(t, 5, corr.Total)
	assert.Equal(t, GradeCritical, corr.Grade)

	assert.Less(t, report.Overall, 80)
}

func TestProductionScoring_CustomWeightsPrioritizeInjection(t *testing.T) {
	events := []models.ArenaEvent{
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 100},
		{GremlinType: "hallucination", Outcome: models.OutcomeSurvived, Score: 95},
		{GremlinType: "injection", Outcome: models.OutcomeCrashed, Score: 0},
		{GremlinType: "injection", Outcome: models.OutcomeCrashed, Score: 10},
		{GremlinType: "identity", Outcome: models.OutcomeCrashed, Score: 5},
	}

	defaultReport := Score(events, nil)

	customWeights := map[Dimension]float64{
		DimensionHallucination: 0.4,
		DimensionLatency:       0.0,
		DimensionCorruption:    0.0,
		DimensionLoop:          0.0,
		DimensionInjection:     0.6,
	}
	customReport := Score(events, customWeights)

	assert.Less(t, customReport.Overall, defaultReport.Overall)
	assert.LessOrEqual(t, customReport.Overall, 45)

	inj := customReport.Dimensions[DimensionInjection]
	assert.Equal(t, 3, inj.Total)
	assert.Equal(t, GradeCritical, inj.Grade)
}
