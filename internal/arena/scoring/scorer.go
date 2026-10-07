package scoring

import (
	"math"
	"sort"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
)

type DimensionScore struct {
	Score    int     `json:"score"`
	Total    int     `json:"total"`
	Survived int     `json:"survived"`
	Degraded int     `json:"degraded"`
	Crashed  int     `json:"crashed"`
	Weight   float64 `json:"weight"`
	Grade    Grade   `json:"grade"`
}

type ResilienceReport struct {
	Overall          int                          `json:"overall"`
	Grade            Grade                        `json:"grade"`
	Dimensions       map[Dimension]DimensionScore `json:"dimensions"`
	Measured         bool                         `json:"measured"`
	UnmappedGremlins []string                     `json:"unmapped_gremlins,omitempty"`
}

func Score(events []models.ArenaEvent, weights map[Dimension]float64) ResilienceReport {
	if weights == nil {
		weights = DefaultWeights
	}

	grouped := make(map[Dimension][]models.ArenaEvent)
	unmappedSet := make(map[string]struct{})
	for _, e := range events {
		if e.Outcome == models.OutcomeUnmeasured {
			continue
		}

		dim, ok := GremlinToDimension[e.GremlinType]
		if !ok {
			unmappedSet[e.GremlinType] = struct{}{}
			continue
		}
		grouped[dim] = append(grouped[dim], e)
	}

	unmapped := make([]string, 0, len(unmappedSet))
	for name := range unmappedSet {
		unmapped = append(unmapped, name)
	}

	sort.Strings(unmapped)

	report := ResilienceReport{
		Dimensions:       make(map[Dimension]DimensionScore),
		UnmappedGremlins: unmapped,
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

	report.Measured = totalWeight > 0
	if !report.Measured {
		report.Grade = GradeNoData
		return report
	}

	report.Overall = int(math.Round(weightedSum / totalWeight))
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
		case models.OutcomeUnmeasured:
		}
		scoreSum += e.Score
	}

	ds.Score = int(math.Round(float64(scoreSum) / float64(ds.Total)))
	ds.Grade = GradeFromScore(ds.Score)
	return ds
}
