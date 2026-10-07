package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/gremlyn-ai/gremlyn/internal/cli"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := newRootCmd()
	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, cli.ErrThresholdNotMet) {
			fmt.Fprintln(os.Stderr, "gremlyn: "+err.Error())
		}
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var logLevel string

	rootCmd := &cobra.Command{
		Use:   "gremlyn",
		Short: "Chaos testing for AI agents",
		Long: `Gremlyn breaks the MCP tools your agent depends on, one failure at a time,
and scores how it copes.

  gremlyn arena ci        Run the scenarios in .gremlyn/arena.yaml against your agent
  gremlyn arena report    Render a report as the Markdown a pull request shows
  gremlyn arena sessions  List recorded chaos runs
  gremlyn wrap            Run an MCP server behind the chaos proxy
  gremlyn version         Print version info

Use "gremlyn [command] --help" for more information about a command.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			lvl, err := zerolog.ParseLevel(logLevel)
			if err != nil {
				return fmt.Errorf("invalid --log-level %q (want debug, info, warn or error)", logLevel)
			}
			zerolog.SetGlobalLevel(lvl)
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "warn", "Log level (debug, info, warn, error)")

	logger := newLogger()

	rootCmd.AddCommand(cli.NewArenaCmd(logger))
	rootCmd.AddCommand(cli.NewWrapCmd(logger))
	rootCmd.AddCommand(cli.NewVersionCmd())

	return rootCmd
}

func newLogger() zerolog.Logger {
	return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		With().
		Timestamp().
		Logger()
}
