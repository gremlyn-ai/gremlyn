package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func NewArenaReplayCmd(logger zerolog.Logger) *cobra.Command {
	var printOnly bool

	cmd := &cobra.Command{
		Use:   "replay <summary.json>",
		Short: "Reconstruct the wrap command from a chaos session summary",
		Long: `Reads a session summary written by 'gremlyn wrap --chaos-summary' and prints the
exact command that reproduces it.

The gremlins are seeded, so the same recipe makes the same injection decisions. The agent
under test is not deterministic, so the SCORE may still differ — replay reproduces the
conditions, not the reaction.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary, err := chaos.ReadSummary(args[0])
			if err != nil {
				return fmt.Errorf("reading summary %q: %w", args[0], err)
			}

			if !summary.Replayable() {
				return fmt.Errorf(
					"summary %q is missing the replay recipe (needs a seed and at least one gremlin); "+
						"it was likely written before the recipe fields existed", args[0])
			}

			command, err := replayCommand(summary)
			if err != nil {
				return err
			}

			if _, err := fmt.Fprintln(cmd.OutOrStdout(), command); err != nil {
				return err
			}

			if !printOnly {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
					"\n# The gremlins replay identically (same seed); the agent may not, so the score can differ.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print", false, "print only the command, with no explanatory note on stderr")
	_ = logger
	return cmd
}

func replayCommand(s chaos.SessionSummary) (string, error) {
	parts := []string{
		"gremlyn wrap",
		"--chaos-gremlins " + shellArg(strings.Join(s.Gremlins, ",")),
		fmt.Sprintf("--chaos-seed %d", s.Seed),
	}
	if s.Intensity != "" {
		parts = append(parts, "--chaos-intensity "+shellArg(s.Intensity))
	}
	if s.Params != (chaos.GremlinParams{}) {
		encoded, err := json.Marshal(s.Params)
		if err != nil {
			return "", fmt.Errorf("encoding params: %w", err)
		}
		parts = append(parts, "--chaos-params "+shellArg(string(encoded)))
	}

	server := s.ServerCommand
	if len(server) == 0 {
		server = []string{"<server command was not recorded — add it here>"}
	}
	parts = append(parts, "-- "+shellJoin(server))

	return strings.Join(parts, " \\\n  "), nil
}

func shellArg(v string) string {
	if v != "" && !strings.ContainsAny(v, " \t\n\"'\\$`|&;<>(){}*?![]#~") {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func shellJoin(argv []string) string {
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellArg(a)
	}
	return strings.Join(quoted, " ")
}
