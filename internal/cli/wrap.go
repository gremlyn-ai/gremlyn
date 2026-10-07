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
	)

	cmd := &cobra.Command{
		Use:   "wrap [flags] -- <command> [args...]",
		Short: "Run an MCP server behind the chaos proxy",
		Long: `Wraps a stdio MCP server process and injects controlled failures into its
tool calls. gremlyn arena ci writes this command into the MCP config it hands the agent.

All stdout output is MCP protocol traffic. Gremlyn logs go to stderr.`,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			childArgs := cmd.ArgsLenAtDash()
			if childArgs < 0 || len(args) == 0 {
				return fmt.Errorf("usage: gremlyn wrap [flags] -- <command> [args...]\n\nMissing child command after '--'")
			}
			childCmd := args[childArgs:]
			if len(childCmd) == 0 {
				return fmt.Errorf("no command specified after '--'")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var signaled atomic.Bool
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				logger.Info().Msg("received shutdown signal")
				signaled.Store(true)
				cancel()
			}()

			name := serverName
			if name == "" {
				name = filepath.Base(childCmd[0])
			}

			proxyCfg := proxy.Config{
				ServerName: name,
				Command:    childCmd[0],
			}
			if len(childCmd) > 1 {
				proxyCfg.Args = childCmd[1:]
			}

			pipeline := proxy.NewPipeline(logger)
			defer pipeline.Close()

			var (
				observer   *chaos.Observer
				eventSink  *chaos.FileEventSink
				sessionRec *chaosSessionRecorder
			)

			var params chaos.GremlinParams
			if chaosParamsJSON != "" {
				if perr := json.Unmarshal([]byte(chaosParamsJSON), &params); perr != nil {
					return fmt.Errorf("parsing --chaos-params: %w", perr)
				}
			}

			if len(chaosGremlins) > 0 {
				gs, err := chaos.BuildGremlinsWithParams(
					chaosGremlins, chaosSeed, chaos.Intensity(chaosIntensity), params)
				if err != nil {
					return fmt.Errorf("configuring chaos: %w", err)
				}

				var sinks []chaos.EventSink
				if chaosEventsPath != "" {
					eventSink, err = chaos.NewFileEventSink(chaosEventsPath)
					if err != nil {
						return fmt.Errorf("configuring chaos events: %w", err)
					}
					defer func() {
						if cerr := eventSink.Close(); cerr != nil {
							logger.Warn().Err(cerr).Msg("closing chaos event file")
						}
					}()
					sinks = append(sinks, eventSink)
				}

				sessionRec = newChaosSessionRecorder(
					ctx, childCmd, chaosGremlins, chaosSeed, chaosIntensity, params, logger)
				if sessionRec != nil {
					sinks = append(sinks, sessionRec)
				}

				observer = chaos.NewObserver(combineEventSinks(sinks), logger, chaos.WithWindow(chaosWindow))
				pipeline.RegisterHandler(chaos.NewGremlinHandler(gs, observer, logger))
				pipeline.RegisterHandler(observer)

				logger.Info().
					Strs("gremlins", chaosGremlins).
					Int64("seed", chaosSeed).
					Str("intensity", chaosIntensity).
					Msg("chaos mode enabled")
			}

			proxyOpts := []proxy.Option{
				proxy.WithLogger(logger),
				proxy.WithPipeline(pipeline),
			}

			p := proxy.NewWrapProxy(proxyCfg, proxyOpts...)

			logger.Info().
				Str("server", name).
				Str("command", childCmd[0]).
				Msg("starting wrap proxy")

			runErr := p.Start(ctx)

			if observer != nil {
				endedNormally := sessionEndedNormally(runErr, ctx.Err(), signaled.Load())
				cov := observer.Finalize(context.WithoutCancel(ctx), endedNormally)
				logger.Info().
					Int("injected", cov.Injected).
					Int("observed", cov.Observed).
					Int("unresolved", cov.Unresolved).
					Bool("complete", cov.Complete()).
					Msg("chaos session finished")

				if chaosSummaryPath != "" {
					summary := chaos.SessionSummary{
						Coverage:      cov,
						Gremlins:      chaosGremlins,
						Seed:          chaosSeed,
						Intensity:     chaosIntensity,
						Params:        params,
						ServerCommand: childCmd,
					}
					if werr := chaos.WriteSummary(chaosSummaryPath, summary); werr != nil {
						logger.Warn().Err(werr).Msg("writing chaos summary")
					}
				}

				if sessionRec != nil {
					sessionRec.finish(context.WithoutCancel(ctx), runErr)
				}
			}

			return runErr
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
	cmd.Flags().DurationVar(&chaosWindow, "chaos-window", chaos.DefaultWindow,
		"How long the agent has to react to an injection before silence is taken as the answer")

	return cmd
}

func sessionEndedNormally(runErr, ctxErr error, signaled bool) bool {
	if signaled {
		return runErr == nil || errors.Is(runErr, context.Canceled)
	}
	return runErr == nil && ctxErr == nil
}
