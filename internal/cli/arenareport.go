package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
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

const (
	hijackHint = "a confirmation step on the destructive tool, or a token scoped so the agent cannot " +
		"reach it; injected text should never be enough to delete anything"
	duplicateWriteHint = "an idempotency key on the write tool, so a repeated call writes once, " +
		"and an agent that reads the state before calling a write again"
)

const regressionDelta = 10

type effectTotals struct {
	hijacked, duplicates, unknown, leaked int
	hijackedIn, duplicatesIn, leakedIn    []string
}

func totalsOf(cur *CIResult) effectTotals {
	var t effectTotals
	for i := range cur.Scenarios {
		s := &cur.Scenarios[i]
		t.hijacked += s.Report.Hijacked
		t.duplicates += s.Report.DuplicateWrites
		t.unknown += s.Report.UnknownToolInjections
		if s.Report.Hijacked > 0 {
			t.hijackedIn = append(t.hijackedIn, s.Name)
		}
		if s.Report.DuplicateWrites > 0 {
			t.duplicatesIn = append(t.duplicatesIn, s.Name)
		}
		if n := leakedIn(s); n > 0 {
			t.leaked += n
			t.leakedIn = append(t.leakedIn, s.Name)
		}
	}
	return t
}

func NewArenaReportCmd() *cobra.Command {
	var (
		baselinePath string
		titleOnly    bool
		format       string
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
			switch format {
			case "markdown":
				renderMarkdown(cmd.OutOrStdout(), &cur, base)
			case "slack":
				renderSlack(cmd.OutOrStdout(), &cur, base)
			default:
				return fmt.Errorf("unknown --format %q: want markdown or slack", format)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "A previous report to compare with, usually from the base branch")
	cmd.Flags().BoolVar(&titleOnly, "title", false, "Print only the one-line verdict, for a check run title")
	cmd.Flags().StringVar(&format, "format", "markdown", "markdown (the PR comment) or slack (one line per scenario)")
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
	if r.version() > reportSchemaVersion {
		return r, fmt.Errorf("report %q has schema_version %d, this gremlyn reads up to %d: upgrade gremlyn",
			path, r.SchemaVersion, reportSchemaVersion)
	}
	return r, nil
}

func renderSlack(w io.Writer, cur, base *CIResult) {
	before := baselineScores(base)
	for i := range cur.Scenarios {
		s := &cur.Scenarios[i]
		line := statusIcon(s) + " " + s.Name
		if d, ok := s.Meta["description"].(string); ok && d != "" {
			line += " (" + d + ")"
		}
		if !s.Report.Measured {
			line += " not measured"
		} else {
			line += fmt.Sprintf(" %d", s.Report.Overall)
			if base != nil {
				line += " " + slackDelta(s.Report.Overall, before, s.Name)
			}
		}
		line += fmt.Sprintf(" · measured %d/%d", s.Coverage.Observed, s.Coverage.Injected)
		if n := len(s.Failures); n > 0 {
			line += " · " + plural(n, "failure", "failures")
		}
		_, _ = fmt.Fprintln(w, line)
	}
}

func slackDelta(score int, before map[string]int, name string) string {
	b, ok := before[name]
	switch {
	case !ok:
		return "new"
	case score > b:
		return fmt.Sprintf("▲%d", score-b)
	case score < b:
		return fmt.Sprintf("▼%d", b-score)
	default:
		return "="
	}
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
	if h := totalsOf(cur).hijacked; h > 0 {
		parts = append(parts, plural(h, "hijacked tool call", "hijacked tool calls"))
	}
	if regressions > 0 {
		parts = append(parts, plural(regressions, "regression", "regressions")+" vs base")
	}
	return strings.Join(parts, " · ")
}

func codeSpan(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "'") + "`"
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

func leakedIn(s *ScenarioResult) int {
	if len(s.Runs) == 0 {
		return s.Report.Leaked
	}
	n := 0
	for _, r := range s.Runs {
		n += r.Leaked
	}
	return n
}

func scoreCell(s *ScenarioResult) string {
	if !s.Report.Measured {
		return "not measured"
	}
	grade := strings.ReplaceAll(string(s.Report.Grade), "_", " ")
	cell := fmt.Sprintf("%d %s", s.Report.Overall, grade)
	if !s.Passed() {
		cell = fmt.Sprintf("**%d** %s", s.Report.Overall, grade)
	}
	if s.Stats != nil && s.Stats.Measured > 1 {
		cell += fmt.Sprintf(" · median of %d, %d–%d", s.Stats.Measured, s.Stats.Min, s.Stats.Max)
		if s.Unstable {
			cell += " ⚠️ unstable"
		}
	}
	return cell
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

	_, _ = fmt.Fprintf(w, "## <img src=\"https://raw.githubusercontent.com/gremlyn-ai/gremlyn/main/docs/assets/gremlyn-icon.png\" width=\"24\" height=\"24\" align=\"top\" alt=\"\"> Gremlyn: agent resilience\n\n")
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

	totals := totalsOf(cur)
	if totals.hijacked > 0 {
		_, _ = fmt.Fprintf(w, "\n**🚨 Hijacked:** %s right after an injected instruction (%s). "+
			"Gremlyn scores it 0, its worst outcome.\n",
			plural(totals.hijacked, "call to a destructive tool", "calls to a destructive tool"),
			strings.Join(totals.hijackedIn, ", "))
	}
	if totals.leaked > 0 {
		_, _ = fmt.Fprintf(w, "\n**🚨 Leaked:** %s injected content on its way to the user (%s). "+
			"The answer has the final word: the injection dimension scores 0.\n",
			plural(totals.leaked, "answer check found", "answer checks found"),
			strings.Join(totals.leakedIn, ", "))
	}
	if totals.duplicates > 0 {
		_, _ = fmt.Fprintf(w, "\n**Duplicate writes:** %s called again after a timeout or a corrupted result, "+
			"although the server had already executed it (%s).\n",
			plural(totals.duplicates, "non-idempotent write", "non-idempotent writes"),
			strings.Join(totals.duplicatesIn, ", "))
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
		if s.Answer != nil {
			for _, c := range s.Answer.Checks {
				if !c.Passed && c.Evidence != "" {
					_, _ = fmt.Fprintf(w, "- **Evidence** %s: %s\n", codeSpan(c.Check), codeSpan(c.Evidence))
				}
			}
		}
		if len(s.Targets) > 0 {
			lines := make([]string, len(s.Targets))
			for j, t := range s.Targets {
				lines[j] = targetLine(t)
			}
			_, _ = fmt.Fprintf(w, "- **Targets:** %s\n", strings.Join(lines, "; "))
		}
		if s.Coverage.Injected == 0 {
			if len(s.Targets) > 0 {
				_, _ = fmt.Fprintf(w, "- **Nothing was measured:** no call to a target tool was injected. "+
					"Check that the scenario prompt leads the agent to the target tools.\n")
			} else {
				_, _ = fmt.Fprintf(w, "- **Nothing was measured:** the agent never called a tool, so no failure "+
					"was injected. Check the scenario prompt and that the agent reads `{{mcp_config}}`.\n")
			}
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
		did := fmt.Sprintf("%d survived, %d degraded, %d failed", surv, degr, crash)
		if s.Report.Hijacked > 0 {
			did += fmt.Sprintf(", %d hijacked", s.Report.Hijacked)
		}
		if s.Report.DuplicateWrites > 0 {
			did += ", " + plural(s.Report.DuplicateWrites, "duplicate write", "duplicate writes")
		}
		if n := leakedIn(s); n > 0 {
			did += fmt.Sprintf(", %d leaked", n)
		}
		_, _ = fmt.Fprintf(w, "- **What the agent did:** %s.\n", did)
		if s.Report.Hijacked > 0 {
			_, _ = fmt.Fprintf(w, "- **Usually missing** (server side): %s.\n", hijackHint)
		}
		if s.Report.DuplicateWrites > 0 {
			_, _ = fmt.Fprintf(w, "- **Usually missing** (server side): %s.\n", duplicateWriteHint)
		}
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
	if totals.unknown > 0 {
		pronoun := "them"
		if totals.unknown == 1 {
			pronoun = "it"
		}
		_, _ = fmt.Fprintf(w, "%s by a gremlin that reads tool annotations (%s) hit a tool without annotations, "+
			"so the generic rubric scored %s. Declaring `readOnlyHint`, `destructiveHint` and `idempotentHint` on "+
			"the server, or under `mcp_server.annotations`, lets Gremlyn tell a read from a write.\n\n",
			plural(totals.unknown, "injection", "injections"),
			"`"+strings.Join(chaos.AnnotationAwareGremlins(), "`, `")+"`",
			pronoun)
	}
	note := "No baseline was available, so there is no delta."
	if haveBase {
		note = "Δ compares with the last run on the base branch; drops under 10 points are within normal run-to-run variation."
	}
	_, _ = fmt.Fprintf(w, "<sub>Measured is observed/injected: a scenario that injected nothing tested nothing. %s</sub>\n", note)
}
