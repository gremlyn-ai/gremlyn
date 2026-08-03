package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// NewStatusCmd creates the "status" subcommand.
func NewStatusCmd(logger zerolog.Logger) *cobra.Command {
	var (
		configPath string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current Gremlyn configuration status",
		Long:  "Loads gremlyn.yaml and displays the configured mode, servers, rules, and global settings.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()

			cfg, err := config.LoadConfig(ctx, configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if jsonOutput {
				return json.NewEncoder(out).Encode(cfg)
			}

			_, _ = fmt.Fprintf(out, "Gremlyn Status\n")
			_, _ = fmt.Fprintf(out, "==============\n\n")
			_, _ = fmt.Fprintf(out, "Config: %s\n", configPath)
			_, _ = fmt.Fprintf(out, "Version: %d\n", cfg.Version)
			if cfg.Mode != "" {
				_, _ = fmt.Fprintf(out, "Mode: %s\n", cfg.Mode)
			}

			_, _ = fmt.Fprintf(out, "\nServers (%d):\n", len(cfg.Servers))
			for name, srv := range cfg.Servers {
				_, _ = fmt.Fprintf(out, "\n  %s:\n", name)
				_, _ = fmt.Fprintf(out, "    Mode: %s\n", srv.Mode)
				if srv.Command != "" {
					_, _ = fmt.Fprintf(out, "    Command: %s\n", srv.Command)
				}
				if srv.Upstream != "" {
					_, _ = fmt.Fprintf(out, "    Upstream: %s\n", srv.Upstream)
				}
				if srv.ListenAddr != "" {
					_, _ = fmt.Fprintf(out, "    Listen: %s\n", srv.ListenAddr)
				}
				_, _ = fmt.Fprintf(out, "    Rules: %d\n", len(srv.Rules))
				for _, rule := range srv.Rules {
					_, _ = fmt.Fprintf(out, "      - %s [%s]\n", rule.Name, rule.Action)
				}
			}

			if cfg.Global.RateLimit != "" || cfg.Global.MaxPayloadSize != "" || len(cfg.Global.AlertChannels) > 0 {
				_, _ = fmt.Fprintf(out, "\nGlobal Settings:\n")
				if cfg.Global.RateLimit != "" {
					_, _ = fmt.Fprintf(out, "  Rate Limit: %s\n", cfg.Global.RateLimit)
				}
				if cfg.Global.MaxPayloadSize != "" {
					_, _ = fmt.Fprintf(out, "  Max Payload: %s\n", cfg.Global.MaxPayloadSize)
				}
				if len(cfg.Global.AlertChannels) > 0 {
					_, _ = fmt.Fprintf(out, "  Alert Channels: %d\n", len(cfg.Global.AlertChannels))
					for _, ch := range cfg.Global.AlertChannels {
						_, _ = fmt.Fprintf(out, "    - %s\n", ch.Type)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "gremlyn.yaml", "Path to gremlyn.yaml")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}
