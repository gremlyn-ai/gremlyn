package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// NewArenaCmd creates the "arena" command group.
func NewArenaCmd(logger zerolog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "arena",
		Short: "Gremlyn Arena — Chaos testing commands",
	}

	cmd.AddCommand(newArenaStatusCmd(logger))
	cmd.AddCommand(newArenaSessionsCmd(logger))
	cmd.AddCommand(newArenaGremlinsCmd(logger))

	return cmd
}

func newArenaStatusCmd(_ zerolog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Arena service status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()
			base := arenaURL()

			body, err := apiGet(ctx, base+"/arena/status")
			if err != nil {
				return fmt.Errorf("arena unreachable at %s: %w", base, err)
			}

			var status struct {
				ActiveSessions    int `json:"active_sessions"`
				GremlinsAvailable int `json:"gremlins_available"`
			}
			if err := json.Unmarshal(body, &status); err != nil {
				_, _ = fmt.Fprintln(out, string(body))
				return nil
			}

			_, _ = fmt.Fprintln(out, "Arena: ONLINE")
			_, _ = fmt.Fprintf(out, "Active sessions: %d\n", status.ActiveSessions)
			_, _ = fmt.Fprintf(out, "Gremlins available: %d\n", status.GremlinsAvailable)
			return nil
		},
	}
}

func newArenaSessionsCmd(_ zerolog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List recent arena sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()
			base := arenaURL()

			body, err := apiGet(ctx, base+"/arena/sessions")
			if err != nil {
				return fmt.Errorf("failed to list sessions: %w", err)
			}

			var resp struct {
				Sessions []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					ServerID string `json:"server_id"`
				} `json:"sessions"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				_, _ = fmt.Fprintln(out, string(body))
				return nil
			}

			if len(resp.Sessions) == 0 {
				_, _ = fmt.Fprintln(out, "No arena sessions.")
				return nil
			}

			_, _ = fmt.Fprintf(out, "%-10s  %-12s  %-16s\n", "ID", "STATUS", "SERVER")
			_, _ = fmt.Fprintln(out, strings.Repeat("-", 42))
			for _, s := range resp.Sessions {
				id := s.ID
				if len(id) > 8 {
					id = id[:8]
				}
				_, _ = fmt.Fprintf(out, "%-10s  %-12s  %-16s\n", id, s.Status, s.ServerID)
			}
			return nil
		},
	}
}

func newArenaGremlinsCmd(_ zerolog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "list-gremlins",
		Short: "Show available gremlin types",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()
			base := arenaURL()

			body, err := apiGet(ctx, base+"/arena/gremlins")
			if err != nil {
				return fmt.Errorf("failed to list gremlins: %w", err)
			}

			var resp struct {
				Gremlins []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
				} `json:"gremlins"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				_, _ = fmt.Fprintln(out, string(body))
				return nil
			}

			if len(resp.Gremlins) == 0 {
				_, _ = fmt.Fprintln(out, "No gremlins available.")
				return nil
			}

			for _, g := range resp.Gremlins {
				_, _ = fmt.Fprintf(out, "  %-16s  %s\n", g.Name, g.Description)
			}
			return nil
		},
	}
}
