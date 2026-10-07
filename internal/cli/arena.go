package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gremlyn-ai/gremlyn/internal/arena/chaos"
	"github.com/gremlyn-ai/gremlyn/internal/arena/storage/filestore"
	"github.com/gremlyn-ai/gremlyn/pkg/datadir"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func NewArenaCmd(logger zerolog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "arena",
		Short: "Chaos testing: break your agent on purpose and score how it copes",
	}

	cmd.AddCommand(newArenaSessionsCmd(logger))
	cmd.AddCommand(newArenaGremlinsCmd())
	cmd.AddCommand(NewArenaCICmd(logger))
	cmd.AddCommand(NewArenaReplayCmd(logger))
	cmd.AddCommand(NewArenaReportCmd())

	return cmd
}

func newArenaSessionsCmd(logger zerolog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List recorded chaos sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()

			dir, err := datadir.Dir()
			if err != nil {
				return fmt.Errorf("resolving data dir: %w", err)
			}
			store, err := filestore.New(filepath.Join(dir, "arena", "sessions"), logger)
			if err != nil {
				return fmt.Errorf("opening session history: %w", err)
			}
			sessions, err := store.ListSessions(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing sessions: %w", err)
			}

			if len(sessions) == 0 {
				_, _ = fmt.Fprintln(out, "No chaos sessions recorded yet. Run `gremlyn arena ci` or `gremlyn wrap --chaos-gremlins ...`.")
				return nil
			}

			_, _ = fmt.Fprintf(out, "%-10s  %-10s  %-20s  %-8s  %s\n", "ID", "STATUS", "STARTED", "INJECTED", "SCORE")
			_, _ = fmt.Fprintln(out, strings.Repeat("-", 64))
			for i := range sessions {
				s := &sessions[i]
				id := s.ID
				if len(id) > 8 {
					id = id[:8]
				}
				_, _ = fmt.Fprintf(out, "%-10s  %-10s  %-20s  %-8d  %s\n",
					id, s.Status, s.StartedAt.Format("2006-01-02 15:04:05"), s.GremlinsSent, scoreOf(s.Results))
			}
			return nil
		},
	}
}

func scoreOf(results json.RawMessage) string {
	var r struct {
		Overall  int    `json:"overall"`
		Grade    string `json:"grade"`
		Measured bool   `json:"measured"`
	}
	if len(results) == 0 || json.Unmarshal(results, &r) != nil || !r.Measured {
		return "not measured"
	}
	return fmt.Sprintf("%d (%s)", r.Overall, r.Grade)
}

func newArenaGremlinsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-gremlins",
		Short: "Show available gremlin types",
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			gs, err := chaos.BuildGremlins(chaos.KnownGremlins(), 1, chaos.IntensityCertain)
			if err != nil {
				return fmt.Errorf("building gremlins: %w", err)
			}
			for _, g := range gs {
				_, _ = fmt.Fprintf(out, "  %-14s  %s\n", g.Name(), g.Description())
			}
			return nil
		},
	}
}
