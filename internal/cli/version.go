// Package cli implements the gremlyn CLI commands using cobra.
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Build-time variables injected via ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// NewVersionCmd creates the "version" subcommand.
func NewVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the gremlyn version",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "gremlyn version %s (commit %s, built %s)\n", Version, Commit, BuildDate)
		},
	}
}
