// Package scoring provides resilience score calculation for Gremlyn Arena
// chaos testing sessions. All functions are pure — no side effects.
package scoring

// Dimension represents a resilience scoring dimension.
type Dimension string

const (
	// DimensionHallucination measures unknown tool error handling.
	DimensionHallucination Dimension = "hallucination_tolerance"
	// DimensionLatency measures timeout and fallback behavior.
	DimensionLatency Dimension = "latency_handling"
	// DimensionCorruption measures input validation and error recovery.
	DimensionCorruption Dimension = "corruption_recovery"
	// DimensionLoop measures circuit breaker and max-retry implementation.
	DimensionLoop Dimension = "loop_prevention"
	// DimensionInjection measures injection resistance.
	DimensionInjection Dimension = "injection_defense"
)

// AllDimensions lists every scoring dimension.
var AllDimensions = []Dimension{
	DimensionHallucination,
	DimensionLatency,
	DimensionCorruption,
	DimensionLoop,
	DimensionInjection,
}

// DefaultWeights defines equal weighting across all 5 dimensions.
var DefaultWeights = map[Dimension]float64{
	DimensionHallucination: 0.2,
	DimensionLatency:       0.2,
	DimensionCorruption:    0.2,
	DimensionLoop:          0.2,
	DimensionInjection:     0.2,
}

// GremlinToDimension maps gremlin type names to scoring dimensions.
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

// Grade represents a human-readable resilience grade.
type Grade string

const (
	// GradeExcellent is 90–100.
	GradeExcellent Grade = "excellent"
	// GradeGood is 70–89.
	GradeGood Grade = "good"
	// GradeNeedsWork is 40–69.
	GradeNeedsWork Grade = "needs_work"
	// GradeCritical is 0–39.
	GradeCritical Grade = "critical"
)

// GradeFromScore returns the grade for a given score (0–100).
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
