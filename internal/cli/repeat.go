package cli

import (
	"fmt"
	"sort"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
)

type RunResult struct {
	Overall  int            `json:"overall"`
	Measured bool           `json:"measured"`
	Coverage chaos.Coverage `json:"coverage"`
	Hijacked int            `json:"hijacked,omitempty"`
	Leaked   int            `json:"leaked,omitempty"`
	Answer   *AnswerResult  `json:"answer,omitempty"`
	AgentErr string         `json:"agent_error,omitempty"`
	Failures []string       `json:"failures,omitempty"`
}

type RunStats struct {
	Median   int `json:"median"`
	Min      int `json:"min"`
	Max      int `json:"max"`
	Spread   int `json:"spread"`
	Measured int `json:"measured"`
}

func repeatsFor(sc ScenarioConfig, flag int) int {
	switch {
	case flag > 0:
		return flag
	case sc.Repeat > 0:
		return sc.Repeat
	default:
		return 1
	}
}

func costLine(scs []ScenarioConfig, flag int) string {
	launches := 0
	for i := range scs {
		launches += repeatsFor(scs[i], flag)
	}
	head := plural(len(scs), "scenario", "scenarios")
	if flag > 1 {
		head += fmt.Sprintf(" × %d runs", flag)
	}
	return head + ", " + plural(launches, "agent launch", "agent launches")
}

func aggregateRuns(runs []ScenarioResult, th ThresholdConfig, strictStability bool) ScenarioResult {
	measured := make([]int, 0, len(runs))
	for i := range runs {
		if runs[i].Report.Measured {
			measured = append(measured, i)
		}
	}
	sort.SliceStable(measured, func(a, b int) bool {
		return runs[measured[a]].Report.Overall < runs[measured[b]].Report.Overall
	})

	pick := 0
	stats := RunStats{Measured: len(measured)}
	if len(measured) > 0 {
		pick = measured[(len(measured)-1)/2]
		stats.Min = runs[measured[0]].Report.Overall
		stats.Max = runs[measured[len(measured)-1]].Report.Overall
		stats.Median = runs[pick].Report.Overall
		stats.Spread = stats.Max - stats.Min
	}

	res := runs[pick]
	res.Failures = append([]string(nil), runs[pick].Failures...)
	res.Stats = &stats
	res.Runs = make([]RunResult, len(runs))
	for i := range runs {
		r := &runs[i]
		res.Runs[i] = RunResult{
			Overall: r.Report.Overall, Measured: r.Report.Measured, Coverage: r.Coverage,
			Hijacked: r.Report.Hijacked, Leaked: r.Report.Leaked, Answer: r.Answer,
			AgentErr: r.AgentErr, Failures: r.Failures,
		}
		if i == pick {
			continue
		}
		extra := r.answerFailures
		if !r.Report.Measured {
			extra = r.Failures
		}
		for _, f := range extra {
			res.Failures = append(res.Failures, fmt.Sprintf("run %d: %s", i+1, f))
		}
	}

	if th.StabilitySpread > 0 && stats.Measured > 1 && stats.Spread > th.StabilitySpread {
		res.Unstable = true
		if strictStability {
			res.Failures = append(res.Failures, fmt.Sprintf(
				"spread %d exceeds the stability_spread of %d over %d runs (%d to %d)",
				stats.Spread, th.StabilitySpread, stats.Measured, stats.Min, stats.Max))
		}
	}
	return res
}
