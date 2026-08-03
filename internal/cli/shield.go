package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// NewShieldCmd creates the "shield" command group.
func NewShieldCmd(logger zerolog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shield",
		Short: "Gremlyn Shield — MCP firewall commands",
	}

	cmd.AddCommand(newShieldStatusCmd(logger))
	cmd.AddCommand(newShieldRulesCmd(logger))
	cmd.AddCommand(newShieldLogsCmd(logger))

	return cmd
}

func newShieldStatusCmd(_ zerolog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Shield running state and connected servers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()
			base := shieldURL()

			body, err := apiGet(ctx, base+"/api/v1/status")
			if err != nil {
				return fmt.Errorf("shield unreachable at %s: %w", base, err)
			}

			var status struct {
				Running bool     `json:"running"`
				Servers []string `json:"servers"`
				Uptime  string   `json:"uptime"`
			}
			if err := json.Unmarshal(body, &status); err != nil {
				_, _ = fmt.Fprintln(out, string(body))
				return nil
			}

			state := "STOPPED"
			if status.Running {
				state = "RUNNING"
			}
			_, _ = fmt.Fprintf(out, "Shield: %s\n", state)
			if status.Uptime != "" {
				_, _ = fmt.Fprintf(out, "Uptime: %s\n", status.Uptime)
			}
			_, _ = fmt.Fprintf(out, "Servers: %d\n", len(status.Servers))
			for _, s := range status.Servers {
				_, _ = fmt.Fprintf(out, "  - %s\n", s)
			}
			return nil
		},
	}
}

func newShieldRulesCmd(_ zerolog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "rules",
		Short: "List all active enforcement rules",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()
			base := shieldURL()

			body, err := apiGet(ctx, base+"/api/v1/rules")
			if err != nil {
				return fmt.Errorf("failed to list rules: %w", err)
			}

			var resp struct {
				Rules []struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Action  string `json:"action"`
					Enabled bool   `json:"enabled"`
				} `json:"rules"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				_, _ = fmt.Fprintln(out, string(body))
				return nil
			}

			if len(resp.Rules) == 0 {
				_, _ = fmt.Fprintln(out, "No rules configured.")
				return nil
			}

			_, _ = fmt.Fprintf(out, "%-8s  %-24s  %-16s  %-8s\n", "ID", "NAME", "ACTION", "ENABLED")
			_, _ = fmt.Fprintln(out, strings.Repeat("-", 62))
			for _, r := range resp.Rules {
				id := r.ID
				if len(id) > 8 {
					id = id[:8]
				}
				_, _ = fmt.Fprintf(out, "%-8s  %-24s  %-16s  %-8v\n", id, r.Name, r.Action, r.Enabled)
			}
			return nil
		},
	}
}

func newShieldLogsCmd(_ zerolog.Logger) *cobra.Command {
	var filterAction string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show recent Shield events",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()
			base := shieldURL()

			path := "/api/v1/events"
			if filterAction == "blocked" {
				path = "/api/v1/events/blocked"
			}

			body, err := apiGet(ctx, base+path)
			if err != nil {
				return fmt.Errorf("failed to list events: %w", err)
			}

			var resp struct {
				Events []struct {
					Timestamp   string `json:"timestamp"`
					ServerID    string `json:"server_id"`
					ToolName    string `json:"tool_name"`
					ActionTaken string `json:"action_taken"`
				} `json:"events"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				_, _ = fmt.Fprintln(out, string(body))
				return nil
			}

			if len(resp.Events) == 0 {
				_, _ = fmt.Fprintln(out, "No events recorded.")
				return nil
			}

			_, _ = fmt.Fprintf(out, "%-22s  %-16s  %-12s  %-10s\n", "TIME", "TOOL", "ACTION", "SERVER")
			_, _ = fmt.Fprintln(out, strings.Repeat("-", 64))
			for _, e := range resp.Events {
				_, _ = fmt.Fprintf(out, "%-22s  %-16s  %-12s  %-10s\n", e.Timestamp, e.ToolName, e.ActionTaken, e.ServerID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&filterAction, "filter", "", "Filter events (e.g., 'blocked')")
	return cmd
}
