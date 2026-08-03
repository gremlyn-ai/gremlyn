package scoring

import (
	"math"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

// DimensionScore holds the score and statistics for a single dimension.
type DimensionScore struct {
	Score    int     `json:"score"`
	Total    int     `json:"total"`
	Survived int     `json:"survived"`
	Degraded int     `json:"degraded"`
	Crashed  int     `json:"crashed"`
	Weight   float64 `json:"weight"`
	Grade    Grade   `json:"grade"`
}

// ResilienceReport is the complete scoring output for a chaos session.
type ResilienceReport struct {
	Overall    int                          `json:"overall"`
	Grade      Grade                        `json:"grade"`
	Dimensions map[Dimension]DimensionScore `json:"dimensions"`
}

// Score calculates a ResilienceReport from arena events.
// This is a pure function with no side effects.
func Score(events []models.ArenaEvent, weights map[Dimension]float64) ResilienceReport {
	if weights == nil {
		weights = DefaultWeights
	}

	// Group events by dimension.
	grouped := make(map[Dimension][]models.ArenaEvent)
	for _, e := range events {
		dim, ok := GremlinToDimension[e.GremlinType]
		if !ok {
			continue
		}
		grouped[dim] = append(grouped[dim], e)
	}

	report := ResilienceReport{
		Dimensions: make(map[Dimension]DimensionScore),
	}

	var weightedSum float64
	var totalWeight float64

	for _, dim := range AllDimensions {
		dimEvents := grouped[dim]
		w := weights[dim]
		ds := scoreDimension(dimEvents, w)
		report.Dimensions[dim] = ds

		if ds.Total > 0 {
			weightedSum += float64(ds.Score) * w
			totalWeight += w
		}
	}

	if totalWeight > 0 {
		report.Overall = int(math.Round(weightedSum / totalWeight))
	}
	report.Grade = GradeFromScore(report.Overall)

	return report
}

func scoreDimension(events []models.ArenaEvent, weight float64) DimensionScore {
	ds := DimensionScore{Weight: weight}

	if len(events) == 0 {
		return ds
	}

	ds.Total = len(events)

	var scoreSum int
	for _, e := range events {
		switch e.Outcome {
		case models.OutcomeSurvived:
			ds.Survived++
		case models.OutcomeDegraded:
			ds.Degraded++
		case models.OutcomeCrashed:
			ds.Crashed++
		}
		scoreSum += e.Score
	}

	ds.Score = int(math.Round(float64(scoreSum) / float64(ds.Total)))
	ds.Grade = GradeFromScore(ds.Score)
	return ds
}
