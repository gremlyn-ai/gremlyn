package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Placeholders substituted into the agent command.
const (
	// PlaceholderMCPConfig is replaced by the path to the generated MCP config.
	PlaceholderMCPConfig = "{{mcp_config}}"
	// PlaceholderPrompt is replaced by the scenario's prompt.
	PlaceholderPrompt = "{{prompt}}"
)

// CIConfig is the .gremlyn/arena.yaml contract.
type CIConfig struct {
	Agent      AgentConfig      `yaml:"agent"`
	MCPServer  MCPServerConfig  `yaml:"mcp_server"`
	Scenarios  []ScenarioConfig `yaml:"scenarios"`
	Thresholds ThresholdConfig  `yaml:"thresholds"`
}

// AgentConfig describes how to launch the agent under test, headlessly.
type AgentConfig struct {
	// Command is the argv. It must contain {{mcp_config}} so the agent is pointed
	// at the proxied server, and usually {{prompt}}.
	Command []string `yaml:"command"`
	// Timeout bounds one scenario run.
	Timeout time.Duration `yaml:"timeout"`
	// Env is added to the agent's environment.
	Env map[string]string `yaml:"env"`
}

// MCPServerConfig is the real MCP server the agent will talk to, through the
// proxy.
type MCPServerConfig struct {
	Name    string   `yaml:"name"`
	Command []string `yaml:"command"`
}

// ScenarioConfig is one chaos run.
type ScenarioConfig struct {
	Name      string   `yaml:"name"`
	Gremlins  []string `yaml:"gremlins"`
	Seed      int64    `yaml:"seed"`
	Intensity string   `yaml:"intensity"`
	Prompt    string   `yaml:"prompt"`
}

// ThresholdConfig is what makes the job pass or fail.
type ThresholdConfig struct {
	MinOverall   int                       `yaml:"min_overall"`
	MinDimension map[scoring.Dimension]int `yaml:"min_dimension"`
}

// ScenarioResult is the outcome of one scenario.
type ScenarioResult struct {
	Name     string                   `json:"name"`
	Report   scoring.ResilienceReport `json:"report"`
	Coverage chaos.Coverage           `json:"coverage"`
	Events   int                      `json:"events"`
	Skipped  int                      `json:"skipped_partial_events"`
	AgentErr string                   `json:"agent_error,omitempty"`
	Failures []string                 `json:"failures,omitempty"`
}

// Passed reports whether the scenario met every threshold.
func (r ScenarioResult) Passed() bool { return len(r.Failures) == 0 }

// CIResult is the whole run.
type CIResult struct {
	Scenarios []ScenarioResult `json:"scenarios"`
	Passed    bool             `json:"passed"`
}

// NewArenaCICmd creates the "arena ci" subcommand.
func NewArenaCICmd(logger zerolog.Logger) *cobra.Command {
	var (
		configPath    string
		outPath       string
		format        string
		keepArtifacts bool
	)

	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Run chaos scenarios against an agent and fail the build on the score",
		Long: `Runs each configured scenario: launches the agent headlessly with its MCP
server proxied through gremlyn in chaos mode, observes how the agent reacts to each
injection, scores the session, and exits non-zero if a threshold is not met.

Needs no running arena service and no database.

A scenario in which the agent never called a tool is a FAILURE, not a pass: no tool
call means no gremlin was crossed and nothing was measured.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadCIConfig(configPath)
			if err != nil {
				return err
			}

			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locating the gremlyn binary: %w", err)
			}

			result := CIResult{Passed: true}
			for _, sc := range cfg.Scenarios {
				res := runScenario(cmd.Context(), logger, self, cfg, sc, keepArtifacts)
				if !res.Passed() {
					result.Passed = false
				}
				result.Scenarios = append(result.Scenarios, res)
			}

			if err := writeCIResult(cmd.OutOrStdout(), outPath, format, result); err != nil {
				return err
			}

			if !result.Passed {
				return errThresholdNotMet
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&configPath, "config", filepath.Join(".gremlyn", "arena.yaml"),
		"Path to the CI scenario config")
	cmd.Flags().StringVar(&outPath, "out", "", "Write the machine-readable report to this file")
	cmd.Flags().StringVar(&format, "format", "text", "Console output format: text or json")
	cmd.Flags().BoolVar(&keepArtifacts, "keep-artifacts", false,
		"Keep the per-scenario temp directory (mcp config, event log) for debugging")

	return cmd
}

// errThresholdNotMet is returned so the process exits non-zero without cobra
// printing a usage block.
var errThresholdNotMet = errors.New("resilience thresholds not met")

func loadCIConfig(path string) (*CIConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	var cfg CIConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}
	if err := validateCIConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &cfg, nil
}

func validateCIConfig(cfg *CIConfig) error {
	if len(cfg.Agent.Command) == 0 {
		return errors.New("agent.command is required")
	}
	// Without this placeholder the agent would talk to its own MCP servers and
	// never cross the proxy, so the run would measure nothing while looking fine.
	if !containsPlaceholder(cfg.Agent.Command, PlaceholderMCPConfig) {
		return fmt.Errorf("agent.command must contain %s, otherwise the agent never talks through the proxy",
			PlaceholderMCPConfig)
	}
	if len(cfg.MCPServer.Command) == 0 {
		return errors.New("mcp_server.command is required")
	}
	if len(cfg.Scenarios) == 0 {
		return errors.New("at least one scenario is required")
	}
	for i, sc := range cfg.Scenarios {
		if sc.Name == "" {
			return fmt.Errorf("scenarios[%d]: name is required", i)
		}
		if len(sc.Gremlins) == 0 {
			return fmt.Errorf("scenario %q: at least one gremlin is required", sc.Name)
		}
		if sc.Intensity != "" && !chaos.Intensity(sc.Intensity).Valid() {
			return fmt.Errorf("scenario %q: unknown intensity %q", sc.Name, sc.Intensity)
		}
		// Fail early rather than after spawning an agent.
		if _, err := chaos.BuildGremlins(sc.Gremlins, sc.Seed, intensityOf(sc)); err != nil {
			return fmt.Errorf("scenario %q: %w", sc.Name, err)
		}
	}
	if cfg.Agent.Timeout <= 0 {
		cfg.Agent.Timeout = 5 * time.Minute
	}
	return nil
}

func intensityOf(sc ScenarioConfig) chaos.Intensity {
	if sc.Intensity == "" {
		return chaos.IntensityMedium
	}
	return chaos.Intensity(sc.Intensity)
}

func containsPlaceholder(argv []string, want string) bool {
	for _, a := range argv {
		if strings.Contains(a, want) {
			return true
		}
	}
	return false
}

// runScenario executes one scenario end to end.
func runScenario(ctx context.Context, logger zerolog.Logger, self string, cfg *CIConfig, sc ScenarioConfig, keep bool) ScenarioResult {
	res := ScenarioResult{Name: sc.Name}

	dir, err := os.MkdirTemp("", "gremlyn-ci-"+sanitize(sc.Name)+"-")
	if err != nil {
		res.Failures = append(res.Failures, fmt.Sprintf("creating temp dir: %v", err))
		return res
	}
	if !keep {
		defer func() { _ = os.RemoveAll(dir) }()
	} else {
		logger.Info().Str("scenario", sc.Name).Str("artifacts", dir).Msg("keeping artifacts")
	}

	eventsPath := filepath.Join(dir, "events.jsonl")
	summaryPath := filepath.Join(dir, "summary.json")
	mcpPath := filepath.Join(dir, "mcp.json")

	seed := sc.Seed
	if seed == 0 {
		seed = gremlins.DefaultSeed
	}

	if err := writeMCPConfig(mcpPath, self, cfg.MCPServer, sc, seed, eventsPath, summaryPath); err != nil {
		res.Failures = append(res.Failures, err.Error())
		return res
	}

	// Run the agent.
	runCtx, cancel := context.WithTimeout(ctx, cfg.Agent.Timeout)
	defer cancel()

	argv := substitute(cfg.Agent.Command, map[string]string{
		PlaceholderMCPConfig: mcpPath,
		PlaceholderPrompt:    sc.Prompt,
	})

	agentCmd := exec.CommandContext(runCtx, argv[0], argv[1:]...) //nolint:gosec // the command is the user's own config
	agentCmd.Env = append(os.Environ(), envSlice(cfg.Agent.Env)...)
	agentCmd.Stdin = nil
	var agentOut strings.Builder
	agentCmd.Stdout = &agentOut
	agentCmd.Stderr = &agentOut

	logger.Info().Str("scenario", sc.Name).Strs("gremlins", sc.Gremlins).Int64("seed", seed).Msg("running scenario")

	agentErr := agentCmd.Run()
	if agentErr != nil {
		// A non-zero agent is worth reporting but is not automatically a
		// scenario failure: an agent may legitimately exit non-zero because a
		// gremlin broke its task, which is the behaviour under test.
		res.AgentErr = agentErr.Error()
		logger.Warn().Err(agentErr).Str("scenario", sc.Name).Msg("agent exited non-zero")
	}

	// Collect what the proxy observed.
	events, skipped, err := chaos.ReadEvents(eventsPath)
	if err != nil {
		res.Failures = append(res.Failures,
			fmt.Sprintf("no observations were recorded: %v (agent output: %s)", err, tail(agentOut.String(), 400)))
		return res
	}
	res.Events = len(events)
	res.Skipped = skipped
	if skipped > 0 {
		logger.Warn().Int("skipped", skipped).Str("scenario", sc.Name).
			Msg("event log had a truncated tail, some observations were lost")
	}

	summary, err := chaos.ReadSummary(summaryPath)
	if err != nil {
		res.Failures = append(res.Failures, fmt.Sprintf("reading coverage summary: %v", err))
		return res
	}
	res.Coverage = summary.Coverage
	res.Report = scoring.Score(events, nil)

	res.Failures = append(res.Failures, checkThresholds(cfg.Thresholds, res)...)
	return res
}

// checkThresholds decides whether a scenario passed.
func checkThresholds(th ThresholdConfig, res ScenarioResult) []string {
	var failures []string

	// Coverage first. A scenario in which no gremlin was crossed measured
	// nothing, and reporting a score for it would be the same defect as the
	// lookup-table outcomes this whole design replaced.
	if res.Coverage.Injected == 0 {
		return []string{
			"no gremlin was ever crossed: the agent made no tool call, so nothing was tested",
		}
	}
	if !res.Coverage.Complete() {
		failures = append(failures, fmt.Sprintf(
			"coverage incomplete (%s): the score covers only part of the session", res.Coverage))
	}

	if th.MinOverall > 0 && res.Report.Overall < th.MinOverall {
		failures = append(failures, fmt.Sprintf(
			"overall resilience %d is below the minimum %d", res.Report.Overall, th.MinOverall))
	}
	for dim, min := range th.MinDimension {
		ds, ok := res.Report.Dimensions[dim]
		if !ok || ds.Total == 0 {
			failures = append(failures, fmt.Sprintf(
				"dimension %q has a threshold but was never exercised", dim))
			continue
		}
		if ds.Score < min {
			failures = append(failures, fmt.Sprintf(
				"dimension %q scored %d, below the minimum %d", dim, ds.Score, min))
		}
	}
	return failures
}

// writeMCPConfig writes the MCP client config that points the agent at this
// binary in chaos mode, wrapping the real server.
func writeMCPConfig(path, self string, srv MCPServerConfig, sc ScenarioConfig, seed int64, eventsPath, summaryPath string) error {
	name := srv.Name
	if name == "" {
		name = "gremlyn-target"
	}

	args := make([]string, 0, 12+len(srv.Command))
	args = append(args,
		"wrap",
		"--chaos-gremlins", strings.Join(sc.Gremlins, ","),
		"--chaos-seed", fmt.Sprintf("%d", seed),
		"--chaos-intensity", string(intensityOf(sc)),
		"--chaos-events", eventsPath,
		"--chaos-summary", summaryPath,
		"--",
	)
	args = append(args, srv.Command...)

	cfg := map[string]any{
		"mcpServers": map[string]any{
			name: map[string]any{
				"command": self,
				"args":    args,
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling MCP config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing MCP config %q: %w", path, err)
	}
	return nil
}

func substitute(argv []string, repl map[string]string) []string {
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		for k, v := range repl {
			a = strings.ReplaceAll(a, k, v)
		}
		out = append(out, a)
	}
	return out
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, s)
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// writeCIResult renders the result for humans and, optionally, for machines.
func writeCIResult(w io.Writer, outPath, format string, result CIResult) error {
	if outPath != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("marshalling report: %w", err)
		}
		if err := os.WriteFile(outPath, data, 0o600); err != nil {
			return fmt.Errorf("writing report %q: %w", outPath, err)
		}
	}

	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return fmt.Errorf("encoding report: %w", err)
		}
		return nil
	}

	for _, sc := range result.Scenarios {
		status := "PASS"
		if !sc.Passed() {
			status = "FAIL"
		}
		_, _ = fmt.Fprintf(w, "\n%s  %s\n", status, sc.Name)
		_, _ = fmt.Fprintf(w, "  resilience: %d (%s)\n", sc.Report.Overall, sc.Report.Grade)
		_, _ = fmt.Fprintf(w, "  coverage:   %s\n", sc.Coverage)
		_, _ = fmt.Fprintf(w, "  events:     %d\n", sc.Events)
		if sc.Skipped > 0 {
			_, _ = fmt.Fprintf(w, "  lost:       %d truncated event(s)\n", sc.Skipped)
		}
		for dim, ds := range sc.Report.Dimensions {
			if ds.Total == 0 {
				continue
			}
			_, _ = fmt.Fprintf(w, "    %-18s %3d  (survived %d, degraded %d, crashed %d)\n",
				dim, ds.Score, ds.Survived, ds.Degraded, ds.Crashed)
		}
		if sc.AgentErr != "" {
			_, _ = fmt.Fprintf(w, "  agent:      exited non-zero: %s\n", sc.AgentErr)
		}
		for _, f := range sc.Failures {
			_, _ = fmt.Fprintf(w, "  ✗ %s\n", f)
		}
	}

	_, _ = fmt.Fprintf(w, "\n")
	if result.Passed {
		_, _ = fmt.Fprintf(w, "All scenarios passed.\n")
	} else {
		_, _ = fmt.Fprintf(w, "Thresholds not met.\n")
	}
	return nil
}
