package scoring

type Dimension string

const (
	DimensionHallucination Dimension = "hallucination_tolerance"
	DimensionLatency       Dimension = "latency_handling"
	DimensionCorruption    Dimension = "corruption_recovery"
	DimensionLoop          Dimension = "loop_prevention"
	DimensionInjection     Dimension = "injection_defense"
)

var AllDimensions = []Dimension{
	DimensionHallucination,
	DimensionLatency,
	DimensionCorruption,
	DimensionLoop,
	DimensionInjection,
}

var DefaultWeights = map[Dimension]float64{
	DimensionHallucination: 0.2,
	DimensionLatency:       0.2,
	DimensionCorruption:    0.2,
	DimensionLoop:          0.2,
	DimensionInjection:     0.2,
}

var GremlinToDimension = map[string]Dimension{
	"hallucination": DimensionHallucination,
	"latency":       DimensionLatency,
	"corruption":    DimensionCorruption,
	"loop":          DimensionLoop,
	"injection":     DimensionInjection,
	"identity":      DimensionInjection,
	"overflow":      DimensionCorruption,
	"timeout":       DimensionLatency,
}

type Grade string

const (
	GradeExcellent Grade = "excellent"
	GradeGood      Grade = "good"
	GradeNeedsWork Grade = "needs_work"
	GradeCritical  Grade = "critical"
	GradeNoData    Grade = "no_data"
)

func GradeFromScore(score int) Grade {
	switch {
	case score >= 90:
		return GradeExcellent
	case score >= 70:
		return GradeGood
	case score >= 40:
		return GradeNeedsWork
	default:
		return GradeCritical
	}
}
