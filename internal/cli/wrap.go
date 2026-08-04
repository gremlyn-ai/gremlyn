package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// NewWrapCmd creates the "wrap" subcommand.
// This is the command that MCP clients invoke to route traffic through gremlyn.
// Usage: gremlyn wrap --config gremlyn.yaml -- npx @modelcontextprotocol/server-memory
func NewWrapCmd(logger zerolog.Logger) *cobra.Command {
	var (
		configPath string
		serverName string

		chaosGremlins    []string
		chaosSeed        int64
		chaosIntensity   string
		chaosEventsPath  string
		chaosSummaryPath string
		chaosWindow      time.Duration
	)

	cmd := &cobra.Command{
		Use:   "wrap [flags] -- <command> [args...]",
		Short: "Run an MCP server through the Gremlyn proxy",
		Long: `Wraps an MCP server process, intercepting all JSON-RPC traffic between
the MCP client and the server. This command is typically invoked by MCP client
configurations rewritten by 'gremlyn init'.

All stdout output is MCP protocol traffic. Gremlyn logs go to stderr.`,
		DisableFlagParsing: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Everything after -- is the child command.
			childArgs := cmd.ArgsLenAtDash()
			if childArgs < 0 || len(args) == 0 {
				return fmt.Errorf("usage: gremlyn wrap --config <path> -- <command> [args...]\n\nMissing child command after '--'")
			}
			childCmd := args[childArgs:]
			if len(childCmd) == 0 {
				return fmt.Errorf("no command specified after '--'")
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle shutdown signals.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				logger.Info().Msg("received shutdown signal")
				cancel()
			}()

			// Load gremlyn config.
			//
			// A missing config is not an error unless the user named one
			// explicitly. `gremlyn wrap -- npx some-server` has to work from any
			// directory: it is the quickstart one-liner and the shape CI uses, and
			// neither has run `gremlyn init`. The config only supplies env vars and
			// a server name, both optional.
			cfg, err := config.LoadConfig(ctx, configPath)
			switch {
			case err == nil:
			case !cmd.Flags().Changed("config") && errors.Is(err, os.ErrNotExist):
				logger.Debug().Str("path", configPath).Msg("no config file, using defaults")
				cfg = &config.Config{Servers: map[string]config.ServerConfig{}}
			default:
				return fmt.Errorf("loading config %q: %w", configPath, err)
			}

			// Determine server name from the command or flag.
			name := serverName
			if name == "" {
				name = inferServerName(childCmd[0], cfg)
			}

			// Build proxy config from gremlyn.yaml server entry + child command.
			proxyCfg := proxy.Config{
				ServerName: name,
				Mode:       models.ServerModeWrap,
				Command:    childCmd[0],
			}
			if len(childCmd) > 1 {
				proxyCfg.Args = childCmd[1:]
			}

			// Merge env from gremlyn.yaml server config if present.
			if srvCfg, ok := cfg.Servers[name]; ok {
				proxyCfg.Env = srvCfg.Env
			}

			// Create pipeline and proxy.
			pipeline := proxy.NewPipeline(logger)

			// Chaos mode. In stdio MCP the agent spawns its own server, so this
			// process — the one the agent launched — is the only place gremlins can
			// sit in the traffic path. Whoever is scoring the session lives in
			// another process and reads the event file we write.
			var (
				observer  *chaos.Observer
				eventSink *chaos.FileEventSink
			)
			if len(chaosGremlins) > 0 {
				gs, err := chaos.BuildGremlins(chaosGremlins, chaosSeed, chaos.Intensity(chaosIntensity))
				if err != nil {
					return fmt.Errorf("configuring chaos: %w", err)
				}

				var events chaos.EventSink
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
					events = eventSink
				}

				observer = chaos.NewObserver(events, logger, chaos.WithWindow(chaosWindow))
				pipeline.RegisterHandler(chaos.NewGremlinHandler(gs, observer, logger))
				pipeline.RegisterHandler(observer)

				logger.Info().
					Strs("gremlins", chaosGremlins).
					Int64("seed", chaosSeed).
					Str("intensity", chaosIntensity).
					Msg("chaos mode enabled")
			}

			p, err := proxy.NewProxy(proxyCfg,
				proxy.WithLogger(logger),
				proxy.WithPipeline(pipeline),
			)
			if err != nil {
				return fmt.Errorf("creating proxy: %w", err)
			}

			logger.Info().
				Str("server", name).
				Str("command", childCmd[0]).
				Msg("starting wrap proxy")

			runErr := p.Start(ctx)

			// Resolve whatever is still being watched and report coverage, so the
			// scorer can tell a fully-measured session from a truncated one.
			if observer != nil {
				endedNormally := runErr == nil && ctx.Err() == nil
				cov := observer.Finalize(context.WithoutCancel(ctx), endedNormally)
				logger.Info().
					Int("injected", cov.Injected).
					Int("observed", cov.Observed).
					Int("unresolved", cov.Unresolved).
					Bool("complete", cov.Complete()).
					Msg("chaos session finished")

				if chaosSummaryPath != "" {
					summary := chaos.SessionSummary{
						Coverage: cov,
						Gremlins: chaosGremlins,
						Seed:     chaosSeed,
					}
					if werr := chaos.WriteSummary(chaosSummaryPath, summary); werr != nil {
						logger.Warn().Err(werr).Msg("writing chaos summary")
					}
				}
			}

			return runErr
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "gremlyn.yaml", "Path to gremlyn.yaml")
	cmd.Flags().StringVar(&serverName, "server", "", "Server name (auto-detected from command if not set)")

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
	cmd.Flags().DurationVar(&chaosWindow, "chaos-window", chaos.DefaultWindow,
		"How long the agent has to react to an injection before silence is taken as the answer")

	return cmd
}

// inferServerName tries to match the child command to a server name in the config.
func inferServerName(command string, cfg *config.Config) string {
	// Check if any server has a matching command.
	for name, srv := range cfg.Servers {
		if srv.Command == command {
			return name
		}
	}
	// Fallback to the command basename.
	return command
}
