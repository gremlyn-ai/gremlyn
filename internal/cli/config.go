package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// NewConfigCmd creates the "config" command group.
func NewConfigCmd(logger zerolog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Gremlyn configuration",
	}

	cmd.AddCommand(newConfigShowCmd(logger))
	cmd.AddCommand(newConfigValidateCmd(logger))

	return cmd
}

func newConfigShowCmd(_ zerolog.Logger) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()

			cfg, err := config.LoadConfig(ctx, configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(cfg)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "gremlyn.yaml", "Path to gremlyn.yaml")
	return cmd
}

func newConfigValidateCmd(_ zerolog.Logger) *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate gremlyn.yaml for errors",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()

			cfg, err := config.LoadConfig(ctx, configPath)
			if err != nil {
				return fmt.Errorf("invalid config: %w", err)
			}

			_, _ = fmt.Fprintf(out, "Config: %s\n", configPath)
			_, _ = fmt.Fprintf(out, "Version: %d\n", cfg.Version)
			_, _ = fmt.Fprintf(out, "Servers: %d\n", len(cfg.Servers))

			for name, srv := range cfg.Servers {
				if srv.Mode == "" {
					_, _ = fmt.Fprintf(out, "  WARNING: server %q has no mode\n", name)
				}
				if srv.Mode == "wrap" && srv.Command == "" {
					_, _ = fmt.Fprintf(out, "  WARNING: server %q is wrap mode but has no command\n", name)
				}
				if srv.Mode == "proxy" && srv.Upstream == "" {
					_, _ = fmt.Fprintf(out, "  WARNING: server %q is proxy mode but has no upstream\n", name)
				}
			}

			_, _ = fmt.Fprintln(out, "\nConfig is valid.")
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "gremlyn.yaml", "Path to gremlyn.yaml")
	return cmd
}
