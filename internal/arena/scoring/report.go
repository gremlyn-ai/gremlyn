package scoring

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FormatJSON returns the report as indented JSON.
func FormatJSON(report ResilienceReport) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

// FormatText returns a human-readable text representation of the report.
func FormatText(report ResilienceReport) string {
	var b strings.Builder

	fmt.Fprintf(&b, "═══════════════════════════════════════\n")
	fmt.Fprintf(&b, "  RESILIENCE REPORT\n")
	fmt.Fprintf(&b, "═══════════════════════════════════════\n\n")
	fmt.Fprintf(&b, "  Overall Score: %d/100  [%s]\n\n", report.Overall, strings.ToUpper(string(report.Grade)))

	for _, dim := range AllDimensions {
		ds, ok := report.Dimensions[dim]
		if !ok {
			continue
		}

		bar := renderBar(ds.Score)
		fmt.Fprintf(&b, "  %-25s %s %3d/100\n", dim, bar, ds.Score)

		if ds.Total > 0 {
			fmt.Fprintf(&b, "  %25s survived=%d  degraded=%d  crashed=%d  (total=%d)\n",
				"", ds.Survived, ds.Degraded, ds.Crashed, ds.Total)
		}
	}

	fmt.Fprintf(&b, "\n═══════════════════════════════════════\n")
	return b.String()
}

func renderBar(score int) string {
	const barWidth = 20
	filled := score * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"
}
