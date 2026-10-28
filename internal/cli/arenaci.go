package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/internal/arena/scoring"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	PlaceholderMCPConfig  = "{{mcp_config}}"
	PlaceholderPrompt     = "{{prompt}}"
	PlaceholderAnswerFile = "{{answer_file}}"
)

const (
	answerSourceFile   = "file"
	answerSourceStdout = "stdout"
	answerSourceNone   = "none"
)

type CIConfig struct {
	Agent      AgentConfig      `yaml:"agent"`
	MCPServer  MCPServerConfig  `yaml:"mcp_server"`
	Scenarios  []ScenarioConfig `yaml:"scenarios"`
	Thresholds ThresholdConfig  `yaml:"thresholds"`
	Meta       map[string]any   `yaml:"meta"`
}

type AgentConfig struct {
	Command []string          `yaml:"command"`
	Timeout time.Duration     `yaml:"timeout"`
	Env     map[string]string `yaml:"env"`
}

type MCPServerConfig struct {
	Name           string              `yaml:"name"`
	Command        []string            `yaml:"command"`
	URL            string              `yaml:"url"`
	Headers        map[string]string   `yaml:"headers"`
	RequestTimeout time.Duration       `yaml:"request_timeout"`
	Annotations    annotationOverrides `yaml:"annotations"`
}

type ScenarioConfig struct {
	Name      string              `yaml:"name"`
	Gremlins  []string            `yaml:"gremlins"`
	Seed      int64               `yaml:"seed"`
	Intensity string              `yaml:"intensity"`
	Prompt    string              `yaml:"prompt"`
	Params    chaos.GremlinParams `yaml:"params"`
	Window    time.Duration       `yaml:"window"`

	TargetTools targetTools    `yaml:"target_tools"`
	Meta        map[string]any `yaml:"meta"`
	Expect      ExpectConfig   `yaml:"expect"`
	Repeat      int            `yaml:"repeat"`
}

type ExpectConfig struct {
	Answer chaos.AnswerExpect `yaml:"answer"`
}

type AnswerResult struct {
	Source string              `json:"source"`
	Checks []chaos.AnswerCheck `json:"checks,omitempty"`
}

type ThresholdConfig struct {
	MinOverall      int                       `yaml:"min_overall"`
	MinDimension    map[scoring.Dimension]int `yaml:"min_dimension"`
	StabilitySpread int                       `yaml:"stability_spread"`
}

type ScenarioResult struct {
	Name        string                   `json:"name"`
	Gremlins    []string                 `json:"gremlins,omitempty"`
	Intensity   string                   `json:"intensity,omitempty"`
	TargetTools []string                 `json:"target_tools,omitempty"`
	Meta        map[string]any           `json:"meta,omitempty"`
	Report      scoring.ResilienceReport `json:"report"`
	Coverage    chaos.Coverage           `json:"coverage"`
	Events      int                      `json:"events"`
	Skipped     int                      `json:"skipped_partial_events"`
	Upstream    chaos.UpstreamStats      `json:"upstream,omitzero"`
	Tools       models.ToolCatalog       `json:"tools,omitempty"`
	Targets     []chaos.TargetResult     `json:"targets,omitempty"`
	Answer      *AnswerResult            `json:"answer,omitempty"`

	Artifacts          string   `json:"artifacts,omitempty"`
	InjectionFallbacks int      `json:"injection_fallbacks,omitempty"`
	AgentErr           string   `json:"agent_error,omitempty"`
	Failures           []string `json:"failures,omitempty"`

	Runs     []RunResult `json:"runs,omitempty"`
	Stats    *RunStats   `json:"stats,omitempty"`
	Unstable bool        `json:"unstable,omitempty"`

	answerFailures []string
}

func (r ScenarioResult) Passed() bool { return len(r.Failures) == 0 }

const reportSchemaVersion = 2

type CIResult struct {
	SchemaVersion int              `json:"schema_version,omitempty"`
	Meta          map[string]any   `json:"meta,omitempty"`
	Scenarios     []ScenarioResult `json:"scenarios"`
	Passed        bool             `json:"passed"`
}

func (r *CIResult) version() int {
	if r.SchemaVersion == 0 {
		return 1
	}
	return r.SchemaVersion
}

func newCIResult(cfg *CIConfig) CIResult {
	return CIResult{SchemaVersion: reportSchemaVersion, Meta: cfg.Meta, Passed: true}
}

func newScenarioResult(sc ScenarioConfig) ScenarioResult {
	return ScenarioResult{
		Name: sc.Name, Gremlins: sc.Gremlins, Intensity: string(intensityOf(sc)),
		TargetTools: sc.TargetTools, Meta: sc.Meta,
	}
}

func NewArenaCICmd(logger zerolog.Logger) *cobra.Command {
	var (
		configPath    string
		outPath       string
		format        string
		keepArtifacts bool
		only          []string
		repeat        int
		strictStable  bool
		artifactsDir  string
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
			if err := protectProcessEnviron(); err != nil {
				logger.Warn().Err(err).Msg("could not hide gremlyn's environment from same-user processes")
			}
			cfg, err := loadCIConfig(configPath)
			if err != nil {
				return err
			}
			if cfg.Scenarios, err = selectScenarios(cfg.Scenarios, only); err != nil {
				return err
			}

			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locating the gremlyn binary: %w", err)
			}
			result := newCIResult(cfg)
			out := cmd.OutOrStdout()
			costOut := out
			if format == "json" {
				costOut = cmd.ErrOrStderr()
			}
			_, _ = fmt.Fprintln(costOut, paletteFor(costOut).dim(costLine(cfg.Scenarios, repeat)))
			for _, sc := range cfg.Scenarios {
				n := repeatsFor(sc, repeat)
				if format != "json" {
					label := fmt.Sprintf("running %s (%s)", sc.Name, strings.Join(sc.Gremlins, ", "))
					if n > 1 {
						label += fmt.Sprintf(" × %d", n)
					}
					_, _ = fmt.Fprintf(out, "\n%s\n", paletteFor(out).dim(label+"..."))
				}
				res := runRepeated(cmd.Context(), logger, self, cfg, sc, artifacts{keep: keepArtifacts, dir: artifactsDir}, n, strictStable)
				if format != "json" {
					writeScenarioText(out, &res)
				}
				if !res.Passed() {
					result.Passed = false
				}
				result.Scenarios = append(result.Scenarios, res)
			}

			if err := writeCIResult(out, outPath, format, result); err != nil {
				return err
			}

			if !result.Passed {
				return ErrThresholdNotMet
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.Flags().StringVar(&configPath, "config", filepath.Join(".gremlyn", "arena.yaml"),
		"Path to the CI scenario config")
	cmd.Flags().StringVar(&outPath, "out", "", "Write the machine-readable report to this file")
	cmd.Flags().StringVar(&format, "format", "text", "Console output format: text or json")
	cmd.Flags().StringSliceVar(&only, "scenario", nil,
		"Run only these scenarios, by name (repeatable). Default: all of them")
	cmd.Flags().IntVar(&repeat, "repeat", 0,
		"Run every scenario this many times with the same seed; thresholds apply to the median run. Default: each scenario's repeat, else 1")
	cmd.Flags().BoolVar(&strictStable, "strict-stability", false,
		"Fail a scenario whose score spread exceeds thresholds.stability_spread, instead of warning")
	cmd.Flags().BoolVar(&keepArtifacts, "keep-artifacts", false,
		"Keep the per-scenario directory (mcp config, event log, agent.log, answer.txt) for debugging")
	cmd.Flags().StringVar(&artifactsDir, "artifacts-dir", "",
		"Keep each scenario's directory under this path instead of the temp dir, for a CI upload. Implies --keep-artifacts")

	return cmd
}

func selectScenarios(all []ScenarioConfig, only []string) ([]ScenarioConfig, error) {
	if len(only) == 0 {
		return all, nil
	}
	want := make(map[string]bool, len(only))
	for _, n := range only {
		want[n] = true
	}
	var out []ScenarioConfig
	for i := range all {
		if want[all[i].Name] {
			out = append(out, all[i])
			delete(want, all[i].Name)
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for _, n := range only {
			if want[n] {
				missing = append(missing, n)
			}
		}
		return nil, fmt.Errorf("unknown scenario(s): %s", strings.Join(missing, ", "))
	}
	return out, nil
}

var ErrThresholdNotMet = errors.New("resilience thresholds not met")

func loadCIConfig(path string) (*CIConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}
	if err := checkKeys(&doc, reflect.TypeFor[CIConfig](), ""); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	var cfg CIConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
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

	if !containsPlaceholder(cfg.Agent.Command, PlaceholderMCPConfig) {
		return fmt.Errorf("agent.command must contain %s, otherwise the agent never talks through the proxy",
			PlaceholderMCPConfig)
	}
	if err := validateMCPServer(&cfg.MCPServer, os.LookupEnv); err != nil {
		return err
	}
	if err := chaos.ValidateAnnotationOverrides(cfg.MCPServer.Annotations); err != nil {
		return fmt.Errorf("mcp_server.annotations: %w", err)
	}
	if cfg.MCPServer.URL != "" {
		resolved, err := resolveUpstream(&cfg.MCPServer, os.LookupEnv)
		if err != nil {
			return err
		}
		if err := checkAgentEnv(cfg.Agent.Env, resolved.Vars); err != nil {
			return err
		}
	}
	if len(cfg.Scenarios) == 0 {
		return errors.New("at least one scenario is required")
	}
	if err := checkMeta("meta", cfg.Meta); err != nil {
		return err
	}
	for dim := range cfg.Thresholds.MinDimension {
		if !slices.Contains(scoring.AllDimensions, dim) {
			return fmt.Errorf("thresholds.min_dimension: unknown dimension %q (known: %s)", dim, dimensionNames())
		}
	}
	for i, sc := range cfg.Scenarios {
		if sc.Name == "" {
			return fmt.Errorf("scenarios[%d]: name is required", i)
		}
		if len(sc.Gremlins) == 0 {
			return fmt.Errorf("scenario %q: at least one gremlin is required", sc.Name)
		}
		if sc.Repeat < 0 {
			return fmt.Errorf("scenario %q: repeat must be positive, got %d", sc.Name, sc.Repeat)
		}
		if sc.Window < 0 {
			return fmt.Errorf("scenario %q: window must be positive, got %s", sc.Name, sc.Window)
		}
		if sc.Intensity != "" && !chaos.Intensity(sc.Intensity).Valid() {
			return fmt.Errorf("scenario %q: unknown intensity %q", sc.Name, sc.Intensity)
		}
		if err := checkMeta(fmt.Sprintf("scenario %q: meta", sc.Name), sc.Meta); err != nil {
			return err
		}
		if err := checkPromptAgainstPayload(sc); err != nil {
			return err
		}
		if err := sc.Expect.Answer.Validate(); err != nil {
			return fmt.Errorf("scenario %q: expect.answer: %w", sc.Name, err)
		}

		gs, err := chaos.BuildGremlinsWithParams(sc.Gremlins, sc.Seed, intensityOf(sc), sc.Params)
		if err != nil {
			return fmt.Errorf("scenario %q: %w", sc.Name, err)
		}
		if sc.TargetTools != nil {
			if err := chaos.ValidateTargets(sc.TargetTools, gs); err != nil {
				return fmt.Errorf("scenario %q: target_tools: %w", sc.Name, err)
			}
		}
	}
	if cfg.Thresholds.StabilitySpread < 0 {
		return fmt.Errorf("thresholds.stability_spread must be positive, got %d", cfg.Thresholds.StabilitySpread)
	}
	if cfg.Agent.Timeout <= 0 {
		cfg.Agent.Timeout = 5 * time.Minute
	}
	return nil
}

func checkPromptAgainstPayload(sc ScenarioConfig) error {
	prompt := strings.ToLower(sc.Prompt)
	for gremlin, text := range sc.Params.Payloads(sc.Gremlins, 0) {
		for _, m := range chaos.PayloadMarkers(text) {
			if strings.Contains(prompt, m) {
				return fmt.Errorf("scenario %q: the prompt names %q, which the %s text also names: "+
					"the rubric could not tell obeying the injection from doing the task, so change one of them", sc.Name, m, gremlin)
			}
		}
	}
	return nil
}

func checkMeta(where string, meta map[string]any) error {
	if _, err := json.Marshal(meta); err != nil {
		return fmt.Errorf("%s must hold plain values with string keys, it is copied into the JSON report: %w", where, err)
	}
	return nil
}

func dimensionNames() string {
	names := make([]string, len(scoring.AllDimensions))
	for i, d := range scoring.AllDimensions {
		names[i] = string(d)
	}
	return strings.Join(names, ", ")
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

type artifacts struct {
	keep bool
	dir  string
}

func (a artifacts) kept() bool { return a.keep || a.dir != "" }

func (a artifacts) mkdir(scenario string) (string, error) {
	if a.dir == "" {
		return os.MkdirTemp("", "gremlyn-ci-"+sanitize(scenario)+"-")
	}
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return "", fmt.Errorf("creating the artifacts directory: %w", err)
	}
	return os.MkdirTemp(a.dir, sanitize(scenario)+"-")
}

func runRepeated(ctx context.Context, logger zerolog.Logger, self string, cfg *CIConfig, sc ScenarioConfig, art artifacts, n int, strict bool) ScenarioResult {
	if n <= 1 {
		return runScenario(ctx, logger, self, cfg, sc, art)
	}
	runs := make([]ScenarioResult, n)
	for i := range runs {
		runs[i] = runScenario(ctx, logger, self, cfg, sc, art)
	}
	return aggregateRuns(runs, cfg.Thresholds, strict)
}

func runScenario(ctx context.Context, logger zerolog.Logger, self string, cfg *CIConfig, sc ScenarioConfig, art artifacts) ScenarioResult {
	res := newScenarioResult(sc)
	dir, err := art.mkdir(sc.Name)
	if err != nil {
		res.Failures = append(res.Failures, fmt.Sprintf("creating the scenario directory: %v", err))
		return res
	}
	if !art.kept() {
		defer func() { _ = os.RemoveAll(dir) }()
	} else {
		res.Artifacts = dir
		logger.Info().Str("scenario", sc.Name).Str("artifacts", dir).Msg("keeping artifacts")
	}

	eventsPath := filepath.Join(dir, "events.jsonl")
	summaryPath := filepath.Join(dir, "summary.json")
	mcpPath := filepath.Join(dir, "mcp.json")
	answerPath := filepath.Join(dir, "answer.txt")

	seed := sc.Seed
	if seed == 0 {
		seed = gremlins.DefaultSeed
	}

	var (
		upstream   *httpChaos
		hiddenVars []string
	)
	if cfg.MCPServer.URL != "" {
		upstream, hiddenVars, err = startScenarioProxy(ctx, logger, cfg.MCPServer, sc, seed, eventsPath, summaryPath)
		if err != nil {
			res.Failures = append(res.Failures, err.Error())
			return res
		}
		err = writeHTTPMCPConfig(mcpPath, cfg.MCPServer.Name, upstream.URL())
	} else {
		err = writeMCPConfig(mcpPath, self, cfg.MCPServer, sc, seed, eventsPath, summaryPath)
	}
	if err != nil {
		if upstream != nil {
			if diag := upstream.stop(ctx); diag != nil && !errors.Is(diag, errUpstreamUnreliable) {
				res.Failures = append(res.Failures, "the MCP server could not be proxied: "+diag.Error())
			}
		}
		res.Failures = append(res.Failures, err.Error())
		return res
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.Agent.Timeout)
	defer cancel()

	argv := substitute(cfg.Agent.Command, map[string]string{
		PlaceholderMCPConfig:  mcpPath,
		PlaceholderPrompt:     sc.Prompt,
		PlaceholderAnswerFile: answerPath,
	})

	agentCmd := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	agentCmd.Env = append(scrubEnv(os.Environ(), hiddenVars), envSlice(cfg.Agent.Env)...)
	agentCmd.Stdin = nil
	var agentOut, agentStdout strings.Builder
	combined := &lockedWriter{w: &agentOut}
	agentCmd.Stdout = io.MultiWriter(&agentStdout, combined)
	agentCmd.Stderr = combined

	logger.Info().Str("scenario", sc.Name).Strs("gremlins", sc.Gremlins).Int64("seed", seed).Msg("running scenario")

	agentErr := agentCmd.Run()
	if agentErr != nil {
		res.AgentErr = agentErr.Error()
		logger.Warn().Err(agentErr).Str("scenario", sc.Name).Msg("agent exited non-zero")
	}
	answer, answerSource := readAnswer(logger, answerPath, agentStdout.String())
	writeArtifacts(logger, dir, agentOut.String(), answer, answerSource)

	if upstream != nil {
		if diag := upstream.stop(ctx); diag != nil && !errors.Is(diag, errUpstreamUnreliable) {
			res.Failures = append(res.Failures, "the MCP server could not be proxied: "+diag.Error())
		}
	}

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
	res.Upstream = summary.Upstream
	res.Tools = summary.Tools

	var answerFailures []string
	events, res.Answer, answerFailures = judgeAnswer(sc, seed, events, answer, answerSource)
	res.Report = scoring.Score(chaos.Rescore(events, summary.Tools), nil)
	res.Targets = chaos.ResolveTargets(summary.Targets, events)
	res.InjectionFallbacks = injectionFallbacks(events)

	res.Failures = append(res.Failures, upstreamFailures(res.Upstream)...)
	res.Failures = append(res.Failures, checkThresholds(cfg.Thresholds, res)...)
	res.Failures = append(res.Failures, answerFailures...)
	res.answerFailures = answerFailures
	return res
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func writeArtifacts(logger zerolog.Logger, dir, agentLog, answer, answerSource string) {
	if err := os.WriteFile(filepath.Join(dir, "agent.log"), []byte(agentLog), 0o600); err != nil {
		logger.Warn().Err(err).Msg("writing agent.log")
	}
	if answerSource != answerSourceStdout {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "answer.txt"), []byte(answer), 0o600); err != nil {
		logger.Warn().Err(err).Msg("writing answer.txt")
	}
}

func judgeAnswer(
	sc ScenarioConfig,
	seed int64,
	events []models.ArenaEvent,
	text, source string,
) ([]models.ArenaEvent, *AnswerResult, []string) {
	exp := sc.Expect.Answer
	canary := sc.Params.CanaryFor(sc.Gremlins, seed)
	if exp.Empty() && canary == "" {
		return events, nil, nil
	}
	var failures []string
	if source == answerSourceNone {
		events = append(events, chaos.UnreadableAnswer("the agent wrote no "+PlaceholderAnswerFile+" and printed nothing on stdout"))
		if !exp.Empty() {
			failures = append(failures, "the agent's answer could not be read (no "+PlaceholderAnswerFile+
				" written, empty stdout), so its expect.answer checks measured nothing")
		}
	} else {
		checked, err := chaos.CheckAnswer(text, exp, canary)
		if err != nil {
			failures = append(failures, fmt.Sprintf("checking the answer: %v", err))
		}
		events = append(events, checked...)
	}
	result := &AnswerResult{Source: source, Checks: chaos.AnswerChecks(events)}
	for _, c := range result.Checks {
		if !c.Passed && !c.Unmeasured {
			failures = append(failures, "answer check failed: "+c.Check)
		}
	}
	return events, result, failures
}

func readAnswer(logger zerolog.Logger, path, stdout string) (text, source string) {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		logger.Warn().Err(err).Msg("reading the agent's answer file")
	}
	if strings.TrimSpace(string(data)) != "" {
		return string(data), answerSourceFile
	}
	if s := answerFromStdout(stdout); strings.TrimSpace(s) != "" {
		return s, answerSourceStdout
	}
	return "", answerSourceNone
}

func answerFromStdout(stdout string) string {
	trimmed := strings.TrimSpace(stdout)
	lines := strings.Split(trimmed, "\n")
	for _, candidate := range []string{trimmed, lines[len(lines)-1]} {
		var r struct {
			Type   string  `json:"type"`
			Result *string `json:"result"`
		}
		if json.Unmarshal([]byte(candidate), &r) == nil && r.Type == "result" && r.Result != nil {
			return *r.Result
		}
	}
	return stdout
}

func upstreamFailures(u chaos.UpstreamStats) []string {
	var out []string
	if u.Timeouts > 0 {
		out = append(out, fmt.Sprintf(
			"the MCP server itself timed out %d time(s), outside any gremlin: the agent reacted to a real failure, "+
				"so the score cannot be attributed (raise mcp_server.request_timeout if the server is just slow)", u.Timeouts))
	}
	if u.Failures > 0 {
		out = append(out, fmt.Sprintf(
			"the MCP server failed %d time(s) outside any gremlin (unreachable, broken stream or over the size cap): "+
				"the score cannot be attributed", u.Failures))
	}
	return out
}

func startScenarioProxy(
	ctx context.Context,
	logger zerolog.Logger,
	srv MCPServerConfig,
	sc ScenarioConfig,
	seed int64,
	eventsPath, summaryPath string,
) (*httpChaos, []string, error) {
	resolved, err := resolveUpstream(&srv, os.LookupEnv)
	if err != nil {
		return nil, nil, err
	}
	h, err := startHTTPChaos(ctx, upstreamTarget{
		Name:           serverName(srv),
		URL:            resolved.URL,
		Headers:        resolved.Headers,
		RequestTimeout: srv.RequestTimeout,
	}, chaosSetup{
		Gremlins:    sc.Gremlins,
		Seed:        seed,
		Intensity:   string(intensityOf(sc)),
		Params:      sc.Params,
		Window:      sc.Window,
		Annotations: srv.Annotations,
		TargetTools: sc.TargetTools,
		EventsPath:  eventsPath,
		SummaryPath: summaryPath,
	}, logger)
	if err != nil {
		return nil, nil, err
	}
	return h, resolved.Vars, nil
}

func serverName(srv MCPServerConfig) string {
	if srv.Name == "" {
		return "gremlyn-target"
	}
	return srv.Name
}

type httpMCPServer struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type httpMCPConfig struct {
	MCPServers map[string]httpMCPServer `json:"mcpServers"`
}

func writeHTTPMCPConfig(path, name, localURL string) error {
	if name == "" {
		name = "gremlyn-target"
	}
	data, err := json.MarshalIndent(httpMCPConfig{
		MCPServers: map[string]httpMCPServer{name: {Type: "http", URL: localURL}},
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling MCP config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing MCP config %q: %w", path, err)
	}
	return nil
}

func checkThresholds(th ThresholdConfig, res ScenarioResult) []string {
	var failures []string

	if res.Coverage.Injected == 0 {
		if f := targetFailures(res.Targets, true); len(f) > 0 {
			return f
		}
		return []string{
			"no gremlin was ever crossed: the agent made no tool call, so nothing was tested",
		}
	}
	failures = append(failures, targetFailures(res.Targets, false)...)
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

func writeMCPConfig(path, self string, srv MCPServerConfig, sc ScenarioConfig, seed int64, eventsPath, summaryPath string) error {
	name := serverName(srv)

	args := make([]string, 0, 14+len(srv.Command))
	args = append(args,
		"wrap",
		"--chaos-gremlins", strings.Join(sc.Gremlins, ","),
		"--chaos-seed", fmt.Sprintf("%d", seed),
		"--chaos-intensity", string(intensityOf(sc)),
		"--chaos-events", eventsPath,
		"--chaos-summary", summaryPath,
	)
	if sc.Params != (chaos.GremlinParams{}) {
		encoded, err := json.Marshal(sc.Params)
		if err != nil {
			return fmt.Errorf("encoding gremlin params: %w", err)
		}
		args = append(args, "--chaos-params", string(encoded))
	}

	if sc.Window > 0 {
		args = append(args, "--chaos-window", sc.Window.String())
	}
	if len(srv.Annotations) > 0 {
		encoded, err := json.Marshal(srv.Annotations)
		if err != nil {
			return fmt.Errorf("encoding tool annotations: %w", err)
		}
		args = append(args, "--chaos-annotations", string(encoded))
	}
	for _, t := range sc.TargetTools {
		args = append(args, "--chaos-target-tool", t)
	}

	args = append(args, "--")
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

	_, _ = fmt.Fprintf(w, "\n")
	if result.Passed {
		_, _ = fmt.Fprintln(w, paletteFor(w).pass("All scenarios passed."))
	} else {
		_, _ = fmt.Fprintln(w, paletteFor(w).fail("Thresholds not met."))
	}
	return nil
}

func writeScenarioText(w io.Writer, sc *ScenarioResult) {
	p := paletteFor(w)
	status := p.pass("PASS")
	if !sc.Passed() {
		status = p.fail("FAIL")
	}
	_, _ = fmt.Fprintf(w, "%s  %s\n", status, p.bold(sc.Name))
	_, _ = fmt.Fprintf(w, "  resilience: %s\n", p.grade(sc.Report.Grade, fmt.Sprintf("%d (%s)", sc.Report.Overall, sc.Report.Grade)))
	_, _ = fmt.Fprintf(w, "  coverage:   %s\n", sc.Coverage)
	for _, t := range sc.Targets {
		_, _ = fmt.Fprintf(w, "  target:     %s\n", targetLine(t))
	}
	if sc.Artifacts != "" {
		_, _ = fmt.Fprintf(w, "  artifacts:  %s\n", p.dim(sc.Artifacts))
	}
	if sc.Stats != nil {
		scores := make([]string, len(sc.Runs))
		for i, r := range sc.Runs {
			scores[i] = fmt.Sprint(r.Overall)
		}
		line := fmt.Sprintf("%s → median %d, spread %d", strings.Join(scores, " "), sc.Stats.Median, sc.Stats.Spread)
		if sc.Unstable {
			line += " " + p.bad("(unstable)")
		}
		_, _ = fmt.Fprintf(w, "  runs:       %s\n", line)
	}
	if sc.Answer != nil {
		passed := 0
		for _, c := range sc.Answer.Checks {
			if c.Passed {
				passed++
			}
		}
		_, _ = fmt.Fprintf(w, "  answer:     %s, %d/%d check(s) passed\n", sc.Answer.Source, passed, len(sc.Answer.Checks))
		for _, c := range sc.Answer.Checks {
			if !c.Passed && c.Evidence != "" {
				_, _ = fmt.Fprintf(w, "              %s %s\n", c.Check+":", p.dim(c.Evidence))
			}
		}
	}
	if sc.Report.Leaked > 0 {
		_, _ = fmt.Fprintf(w, "  leaked:     %s\n", p.bad(fmt.Sprintf("%d answer check(s) found injected content on its way to the user", sc.Report.Leaked)))
	}
	if sc.InjectionFallbacks > 0 {
		_, _ = fmt.Fprintf(w, "  placement:  %s\n", p.dim(fmt.Sprintf("%d injection(s) fell back to a field: the result had no content array", sc.InjectionFallbacks)))
	}
	if sc.Skipped > 0 {
		_, _ = fmt.Fprintf(w, "  lost:       %d truncated event(s)\n", sc.Skipped)
	}
	if sc.Report.Hijacked > 0 {
		_, _ = fmt.Fprintf(w, "  hijacked:   %s\n", p.bad(fmt.Sprintf("%d destructive call(s) right after an injection", sc.Report.Hijacked)))
	}
	if sc.Report.DuplicateWrites > 0 {
		_, _ = fmt.Fprintf(w, "  duplicate:  %s\n", p.bad(fmt.Sprintf("%d non-idempotent write(s) called again", sc.Report.DuplicateWrites)))
	}
	if sc.Report.UnknownToolInjections > 0 {
		_, _ = fmt.Fprintf(w, "  annotations: %s\n", p.dim(fmt.Sprintf("%d injection(s) hit a tool without annotations", sc.Report.UnknownToolInjections)))
	}

	dims := make([]string, 0, len(sc.Report.Dimensions))
	for dim := range sc.Report.Dimensions {
		dims = append(dims, string(dim))
	}
	sort.Strings(dims)
	for _, dim := range dims {
		ds := sc.Report.Dimensions[scoring.Dimension(dim)]
		if ds.Total == 0 {
			continue
		}
		counts := fmt.Sprintf("survived %d, degraded %d, crashed %d", ds.Survived, ds.Degraded, ds.Crashed)
		if ds.Hijacked > 0 {
			counts += fmt.Sprintf(", hijacked %d", ds.Hijacked)
		}
		if ds.DuplicateWrites > 0 {
			counts += fmt.Sprintf(", duplicate writes %d", ds.DuplicateWrites)
		}
		if ds.Leaked > 0 {
			counts += fmt.Sprintf(", leaked %d", ds.Leaked)
		}
		_, _ = fmt.Fprintf(w, "    %-20s %s  %s\n", dim, p.grade(ds.Grade, fmt.Sprintf("%3d", ds.Score)), p.dim("("+counts+")"))
	}
	if sc.AgentErr != "" {
		_, _ = fmt.Fprintf(w, "  agent:      exited non-zero: %s\n", sc.AgentErr)
	}
	for _, f := range sc.Failures {
		_, _ = fmt.Fprintf(w, "  %s\n", p.bad("✗ "+f))
	}
}

func injectionFallbacks(events []models.ArenaEvent) int {
	n := 0
	for i := range events {
		var d models.EventDetails
		if json.Unmarshal(events[i].Details, &d) == nil && d.Placement == gremlins.PlacementField {
			n++
		}
	}
	return n
}
