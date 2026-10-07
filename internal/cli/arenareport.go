package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var fixHints = map[string]string{
	"timeout":       "a per-call timeout and a bounded retry with backoff",
	"latency":       "patience: wait for the slow result instead of calling the tool again",
	"corruption":    "validation of the result's shape before using it, and a retry when it is invalid",
	"overflow":      "a size check on the result before it reaches the model",
	"hallucination": "handling of an unknown-tool error instead of crashing or looping",
	"loop":          "a cap on repeated calls to the same tool",
	"injection":     "tool output treated as data, never as instructions",
	"identity":      "tool output treated as data, never as instructions",
}

const regressionDelta = 10

func NewArenaReportCmd() *cobra.Command {
	var (
		baselinePath string
		titleOnly    bool
	)
	cmd := &cobra.Command{
		Use:   "report <report.json>",
		Short: "Render an arena ci report as Markdown, optionally compared with a baseline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cur, err := readCIResult(args[0])
			if err != nil {
				return err
			}
			var base *CIResult
			if baselinePath != "" {
				b, err := readCIResult(baselinePath)
				if err != nil {
					return err
				}
				base = &b
			}
			if titleOnly {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), reportTitle(&cur, base))
				return nil
			}
			renderMarkdown(cmd.OutOrStdout(), &cur, base)
			return nil
		},
	}
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "A previous report to compare with, usually from the base branch")
	cmd.Flags().BoolVar(&titleOnly, "title", false, "Print only the one-line verdict, for a check run title")
	return cmd
}

func readCIResult(path string) (CIResult, error) {
	var r CIResult
	data, err := os.ReadFile(path)
	if err != nil {
		return r, fmt.Errorf("reading report %q: %w", path, err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parsing report %q: %w", path, err)
	}
	return r, nil
}

func baselineScores(base *CIResult) map[string]int {
	out := map[string]int{}
	if base == nil {
		return out
	}
	for i := range base.Scenarios {
		s := &base.Scenarios[i]
		if s.Report.Measured {
			out[s.Name] = s.Report.Overall
		}
	}
	return out
}

func reportTitle(cur, base *CIResult) string {
	before := baselineScores(base)
	failing, regressions := 0, 0
	for i := range cur.Scenarios {
		s := &cur.Scenarios[i]
		if !s.Passed() {
			failing++
		}
		if b, ok := before[s.Name]; ok && s.Report.Measured && b-s.Report.Overall >= regressionDelta {
			regressions++
		}
	}
	n := len(cur.Scenarios)
	var parts []string
	switch {
	case n == 0:
		return "No scenario ran"
	case failing == 0:
		parts = append(parts, fmt.Sprintf("All %d scenarios passed", n))
	default:
		parts = append(parts, fmt.Sprintf("%d of %d scenarios below threshold", failing, n))
	}
	if regressions > 0 {
		parts = append(parts, plural(regressions, "regression", "regressions")+" vs base")
	}
	return strings.Join(parts, " · ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func statusIcon(s *ScenarioResult) string {
	switch {
	case s.Passed():
		return "✅"
	case s.Coverage.Injected == 0 || !s.Report.Measured:
		return "⚠️"
	default:
		return "❌"
	}
}

func scoreCell(s *ScenarioResult) string {
	if !s.Report.Measured {
		return "not measured"
	}
	grade := strings.ReplaceAll(string(s.Report.Grade), "_", " ")
	if s.Passed() {
		return fmt.Sprintf("%d %s", s.Report.Overall, grade)
	}
	return fmt.Sprintf("**%d** %s", s.Report.Overall, grade)
}

func deltaCell(s *ScenarioResult, before map[string]int, haveBase bool) string {
	if !haveBase {
		return ""
	}
	b, ok := before[s.Name]
	if !ok || !s.Report.Measured {
		return "new"
	}
	d := s.Report.Overall - b
	switch {
	case d == 0:
		return "="
	case d <= -regressionDelta:
		return fmt.Sprintf("📉 %+d", d)
	default:
		return fmt.Sprintf("%+d", d)
	}
}

func renderMarkdown(w io.Writer, cur, base *CIResult) {
	before := baselineScores(base)
	haveBase := base != nil

	injected, observed := 0, 0
	for i := range cur.Scenarios {
		injected += cur.Scenarios[i].Coverage.Injected
		observed += cur.Scenarios[i].Coverage.Observed
	}

	_, _ = fmt.Fprintf(w, "## 🧪 Gremlyn: agent resilience\n\n")
	_, _ = fmt.Fprintf(w, "**%s** · %d of %d injected failures observed\n\n",
		reportTitle(cur, base), observed, injected)

	_, _ = fmt.Fprintf(w, "| | Scenario | Gremlins | Score | Δ vs base | Measured |\n")
	_, _ = fmt.Fprintf(w, "|---|---|---|---:|---:|---|\n")
	for i := range cur.Scenarios {
		s := &cur.Scenarios[i]
		_, _ = fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %d/%d |\n",
			statusIcon(s), s.Name, strings.Join(s.Gremlins, ", "), scoreCell(s),
			deltaCell(s, before, haveBase), s.Coverage.Observed, s.Coverage.Injected)
	}

	var failing []string
	for i := range cur.Scenarios {
		s := &cur.Scenarios[i]
		if s.Passed() {
			continue
		}
		failing = append(failing, s.Name)
		_, _ = fmt.Fprintf(w, "\n### %s %s\n\n", statusIcon(s), s.Name)
		for _, f := range s.Failures {
			_, _ = fmt.Fprintf(w, "- %s\n", f)
		}
		if s.Coverage.Injected == 0 {
			_, _ = fmt.Fprintf(w, "- **Nothing was measured:** the agent never called a tool, so no failure "+
				"was injected. Check the scenario prompt and that the agent reads `{{mcp_config}}`.\n")
			if s.AgentErr != "" {
				_, _ = fmt.Fprintf(w, "- Agent exited with: `%s`\n", s.AgentErr)
			}
			continue
		}
		var surv, degr, crash int
		for _, d := range s.Report.Dimensions {
			surv += d.Survived
			degr += d.Degraded
			crash += d.Crashed
		}
		_, _ = fmt.Fprintf(w, "- **What the agent did:** %d survived, %d degraded, %d failed.\n", surv, degr, crash)
		for _, g := range s.Gremlins {
			if hint, ok := fixHints[g]; ok {
				_, _ = fmt.Fprintf(w, "- **Usually missing** (`%s`): %s.\n", g, hint)
			}
		}
	}

	_, _ = fmt.Fprintf(w, "\n---\n")
	if len(failing) > 0 {
		flags := make([]string, 0, len(failing))
		for _, n := range failing {
			flags = append(flags, "--scenario "+n)
		}
		_, _ = fmt.Fprintf(w, "**Fix it:** run `/gremlyn:chaos-test` in Claude Code to get the change made, "+
			"or reproduce locally with `gremlyn arena ci %s`.\n\n", strings.Join(flags, " "))
	}
	note := "No baseline was available, so there is no delta."
	if haveBase {
		note = "Δ compares with the last run on the base branch; drops under 10 points are within normal run-to-run variation."
	}
	_, _ = fmt.Fprintf(w, "<sub>Measured is observed/injected: a scenario that injected nothing tested nothing. %s</sub>\n", note)
}
