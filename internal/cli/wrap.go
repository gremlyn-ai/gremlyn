package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
			cfg, err := config.LoadConfig(ctx, configPath)
			if err != nil {
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

			return p.Start(ctx)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "gremlyn.yaml", "Path to gremlyn.yaml")
	cmd.Flags().StringVar(&serverName, "server", "", "Server name (auto-detected from command if not set)")

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
