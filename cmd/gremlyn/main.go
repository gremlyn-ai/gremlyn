package main

import (
	"os"

	"github.com/gremlyn-ai/gremlyn/internal/cli"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var logLevel string

	rootCmd := &cobra.Command{
		Use:   "gremlyn",
		Short: "Gremlyn — Security & resilience for AI agents",
		Long: `Gremlyn is a security and resilience platform for AI agents.

  gremlyn init      Scan MCP configs and generate gremlyn.yaml
  gremlyn wrap      Run an MCP server through the Gremlyn proxy
  gremlyn status    Show current configuration
  gremlyn doctor    Run diagnostic checks
  gremlyn version   Print version info

Use "gremlyn [command] --help" for more information about a command.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			_ = logLevel
		},
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	logger := newLogger(logLevel)

	rootCmd.AddCommand(cli.NewInitCmd(logger))
	rootCmd.AddCommand(cli.NewWrapCmd(logger))
	rootCmd.AddCommand(cli.NewStatusCmd(logger))
	rootCmd.AddCommand(cli.NewDoctorCmd(logger))
	rootCmd.AddCommand(cli.NewVersionCmd())
	rootCmd.AddCommand(cli.NewShieldCmd(logger))
	rootCmd.AddCommand(cli.NewArenaCmd(logger))
	rootCmd.AddCommand(cli.NewConfigCmd(logger))

	return rootCmd
}

func newLogger(level string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		Level(lvl).
		With().
		Timestamp().
		Logger()
}
