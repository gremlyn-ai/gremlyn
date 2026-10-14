package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func NewWrapCmd(logger zerolog.Logger) *cobra.Command {
	var (
		serverName       string
		chaosGremlins    []string
		chaosSeed        int64
		chaosIntensity   string
		chaosEventsPath  string
		chaosSummaryPath string
		chaosWindow      time.Duration
		chaosParamsJSON  string
		chaosAnnotations string
		upstreamURL      string
		upstreamHeaders  []string
		upstreamTimeout  time.Duration
		listenAddr       string
	)

	cmd := &cobra.Command{
		Use:   "wrap [flags] -- <command> [args...] | wrap --upstream <url> [flags]",
		Short: "Run an MCP server behind the chaos proxy",
		Long: `Wraps a stdio MCP server process, or fronts a Streamable HTTP MCP server with
--upstream, and injects controlled failures into its tool calls.

stdio: gremlyn arena ci writes this command into the MCP config it hands the agent.
All stdout output is MCP protocol traffic. Gremlyn logs go to stderr.

--upstream: the proxy listens on a loopback address and prints its URL on stderr;
point the agent's MCP client at it. --upstream-header values expand ${VAR} from the
environment, so a token can stay out of the command line: single-quote the flag.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var params chaos.GremlinParams
			if chaosParamsJSON != "" {
				if perr := json.Unmarshal([]byte(chaosParamsJSON), &params); perr != nil {
					return fmt.Errorf("parsing --chaos-params: %w", perr)
				}
			}
			var annotations map[string]models.ToolAnnotations
			if chaosAnnotations != "" {
				if len(chaosGremlins) == 0 {
					return errors.New("--chaos-annotations applies with --chaos-gremlins only: without a gremlin nothing is scored")
				}
				parsed, aerr := chaos.ParseAnnotationOverrides([]byte(chaosAnnotations))
				if aerr != nil {
					return fmt.Errorf("parsing --chaos-annotations: %w", aerr)
				}
				annotations = parsed
			}
			setup := chaosSetup{
				Gremlins:    chaosGremlins,
				Seed:        chaosSeed,
				Intensity:   chaosIntensity,
				Params:      params,
				Window:      chaosWindow,
				Annotations: annotations,
				EventsPath:  chaosEventsPath,
				SummaryPath: chaosSummaryPath,
			}

			if upstreamURL != "" {
				if len(args) > 0 {
					return errors.New("--upstream fronts an HTTP server: drop the command after '--'")
				}
				if upstreamTimeout <= 0 {
					return fmt.Errorf("--upstream-timeout must be positive, got %s", upstreamTimeout)
				}
				headers, err := parseHeaderFlags(upstreamHeaders)
				if err != nil {
					return err
				}
				headers, err = resolveHeaders(headers, os.LookupEnv, nil)
				if err != nil {
					return fmt.Errorf("--upstream-header: %w", err)
				}
				target, err := resolveURL(upstreamURL, os.LookupEnv, nil)
				if err != nil {
					return fmt.Errorf("--upstream: %w", err)
				}
				name := serverName
				if name == "" {
					name = "upstream"
				}
				return runUpstreamWrap(cmd, logger, upstreamTarget{
					Name:           name,
					URL:            target,
					Headers:        headers,
					ListenAddr:     listenAddr,
					RequestTimeout: upstreamTimeout,
				}, setup)
			}

			if len(upstreamHeaders) > 0 || cmd.Flags().Changed("listen") || cmd.Flags().Changed("upstream-timeout") {
				return errors.New("--upstream-header, --listen and --upstream-timeout apply with --upstream only")
			}

			childArgs := cmd.ArgsLenAtDash()
			if childArgs < 0 || len(args) == 0 {
				return fmt.Errorf("usage: gremlyn wrap [flags] -- <command> [args...]\n\nMissing child command after '--'")
			}
			childCmd := args[childArgs:]
			if len(childCmd) == 0 {
				return fmt.Errorf("no command specified after '--'")
			}
			setup.ServerCommand = childCmd
			return runStdioWrap(logger, serverName, childCmd, setup)
		},
	}

	cmd.Flags().StringVar(&serverName, "server", "", "Server name in logs (default: the command's base name)")

	cmd.Flags().StringSliceVar(&chaosGremlins, "chaos-gremlins", nil,
		"Enable chaos mode with these gremlins, in order (e.g. corruption,latency). "+
			"Order matters: the first gremlin to fire on a message wins")
	cmd.Flags().Int64Var(&chaosSeed, "chaos-seed", gremlins.DefaultSeed,
		"Seed for the gremlins. The same seed replays the same session")
	cmd.Flags().StringVar(&chaosIntensity, "chaos-intensity", string(chaos.IntensityMedium),
		"Injection intensity: low, medium, high or certain")
	cmd.Flags().StringVar(&chaosEventsPath, "chaos-events", "",
		"Append resolved arena events to this file as JSON Lines")
	cmd.Flags().StringVar(&chaosSummaryPath, "chaos-summary", "",
		"Write the session's coverage summary to this file as JSON")
	cmd.Flags().StringVar(&chaosParamsJSON, "chaos-params", "",
		`JSON overriding the gremlins' bounds, e.g. '{"min_delay_ms":8000,"max_delay_ms":12000}'. `+
			`Fields: min/max_delay_ms, min/max_timeout_ms, payload_bytes, max_loops, fake_tool, `+
			`injection_text, identity_text, loop_message`)
	cmd.Flags().StringVar(&chaosAnnotations, "chaos-annotations", "",
		`JSON declaring tool effects the server does not annotate, scored as if the server had, e.g. `+
			`'{"create_note":{"readOnlyHint":false,"idempotentHint":false}}'. Hints: readOnlyHint, destructiveHint, idempotentHint`)
	cmd.Flags().DurationVar(&chaosWindow, "chaos-window", chaos.DefaultWindow,
		"How long the agent has to react to an injection before silence is taken as the answer")

	cmd.Flags().StringVar(&upstreamURL, "upstream", "",
		"Front this Streamable HTTP MCP endpoint instead of a stdio command: https, or http on a loopback address")
	cmd.Flags().StringArrayVar(&upstreamHeaders, "upstream-header", nil,
		`Header sent to the upstream, "Name: value" (repeatable). ${VAR} expands from the environment`)
	cmd.Flags().DurationVar(&upstreamTimeout, "upstream-timeout", proxy.DefaultRequestTimeout,
		"How long the upstream may stay silent on a request before the agent gets a 504")
	cmd.Flags().StringVar(&listenAddr, "listen", proxy.DefaultListenAddr,
		"Loopback address the HTTP proxy listens on; port 0 picks a free one")

	return cmd
}

func runStdioWrap(logger zerolog.Logger, serverName string, childCmd []string, setup chaosSetup) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var signaled atomic.Bool
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			logger.Info().Msg("received shutdown signal")
			signaled.Store(true)
			cancel()
		case <-ctx.Done():
		}
	}()

	name := serverName
	if name == "" {
		name = filepath.Base(childCmd[0])
	}
	proxyCfg := proxy.Config{ServerName: name, Command: childCmd[0]}
	if len(childCmd) > 1 {
		proxyCfg.Args = childCmd[1:]
	}

	run, err := newChaosRun(ctx, setup, logger)
	if err != nil {
		return err
	}
	defer run.close()

	p := proxy.NewWrapProxy(proxyCfg, proxy.WithLogger(logger), proxy.WithPipeline(run.pipeline))
	logger.Info().Str("server", name).Str("command", childCmd[0]).Msg("starting wrap proxy")

	runErr := p.Start(ctx)
	run.finish(ctx, runErr, sessionEndedNormally(runErr, ctx.Err(), signaled.Load()), chaos.UpstreamStats{})
	return runErr
}

func runUpstreamWrap(cmd *cobra.Command, logger zerolog.Logger, target upstreamTarget, setup chaosSetup) error {
	if err := protectProcessEnviron(); err != nil {
		logger.Warn().Err(err).Msg("could not hide gremlyn's environment from same-user processes")
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	h, err := startHTTPChaos(ctx, target, setup, logger)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "gremlyn: proxying %s at %s\n", h.run.setup.ServerURL, h.URL())

	<-ctx.Done()
	logger.Info().Msg("shutting down the http proxy")
	err = h.stop(ctx)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errUpstreamUnreliable):
		logger.Warn().Err(err).Msg("the session is recorded as failed")
		return err
	default:
		return fmt.Errorf("the MCP server could not be proxied: %w", err)
	}
}

func sessionEndedNormally(runErr, ctxErr error, signaled bool) bool {
	if signaled {
		return runErr == nil || errors.Is(runErr, context.Canceled)
	}
	return runErr == nil && ctxErr == nil
}
