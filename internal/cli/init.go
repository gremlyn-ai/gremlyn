package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// initJSONOutput is the typed structure for JSON output of the init command.
type initJSONOutput struct {
	Configs      []config.MCPClientConfig `json:"configs"`
	TotalServers int                      `json:"total_servers"`
	DryRun       bool                     `json:"dry_run"`
}

// NewInitCmd creates the "init" subcommand.
func NewInitCmd(logger zerolog.Logger) *cobra.Command {
	var (
		mcpConfigPath string
		outputPath    string
		dryRun        bool
		jsonOutput    bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scan MCP config and generate gremlyn.yaml",
		Long:  "Detects MCP client configurations, generates a gremlyn.yaml config file, and rewrites MCP configs to route through Gremlyn.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()

			// Detect MCP configs.
			var configs []config.MCPClientConfig
			var err error

			if mcpConfigPath != "" {
				cfg, parseErr := parseSingleMCPConfig(mcpConfigPath)
				if parseErr != nil {
					return fmt.Errorf("parsing MCP config %q: %w", mcpConfigPath, parseErr)
				}
				configs = append(configs, *cfg)
			} else {
				configs, err = config.DetectMCPConfigs(ctx)
				if err != nil {
					return fmt.Errorf("detecting MCP configs: %w", err)
				}
			}

			if len(configs) == 0 {
				_, _ = fmt.Fprintln(out, "No MCP client configurations found.")
				_, _ = fmt.Fprintln(out, "Searched: ~/.claude/claude_desktop_config.json, ~/.cursor/mcp.json, ./mcp.json")
				_, _ = fmt.Fprintln(out, "Set GREMLYN_MCP_CONFIG to specify a custom path.")
				return nil
			}

			// Count total servers.
			totalServers := 0
			for _, cfg := range configs {
				totalServers += len(cfg.Servers)
			}

			if jsonOutput {
				return json.NewEncoder(out).Encode(initJSONOutput{
					Configs:      configs,
					TotalServers: totalServers,
					DryRun:       dryRun,
				})
			}

			// Display detected servers.
			_, _ = fmt.Fprintf(out, "Found %d MCP server(s) across %d config(s):\n\n", totalServers, len(configs))
			for _, cfg := range configs {
				_, _ = fmt.Fprintf(out, "  %s (%s):\n", cfg.ClientName, cfg.Path)
				for name, srv := range cfg.Servers {
					if srv.URL != "" {
						_, _ = fmt.Fprintf(out, "    - %s (HTTP: %s)\n", name, srv.URL)
					} else {
						_, _ = fmt.Fprintf(out, "    - %s (stdio: %s)\n", name, srv.Command)
					}
				}
				_, _ = fmt.Fprintln(out)
			}

			if dryRun {
				_, _ = fmt.Fprintln(out, "[dry-run] Would generate gremlyn.yaml and rewrite MCP configs.")
				gremlynCfg := config.GenerateDefaultGremlynYAML(configs)
				yamlData, _ := yaml.Marshal(gremlynCfg)
				_, _ = fmt.Fprintf(out, "--- gremlyn.yaml ---\n%s", string(yamlData))
				return nil
			}

			// Generate gremlyn.yaml.
			gremlynCfg := config.GenerateDefaultGremlynYAML(configs)
			yamlData, err := yaml.Marshal(gremlynCfg)
			if err != nil {
				return fmt.Errorf("marshaling gremlyn.yaml: %w", err)
			}

			if err := os.WriteFile(outputPath, yamlData, 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", outputPath, err)
			}
			_, _ = fmt.Fprintf(out, "Generated %s\n", outputPath)

			// Backup and rewrite MCP configs.
			gremlynBinary, _ := os.Executable()
			if gremlynBinary == "" {
				gremlynBinary = "gremlyn"
			}

			for i, cfg := range configs {
				backupPath, backupErr := config.BackupConfig(cfg.Path)
				if backupErr != nil {
					logger.Warn().Err(backupErr).Str("path", cfg.Path).Msg("failed to backup config")
					continue
				}
				_, _ = fmt.Fprintf(out, "Backed up %s -> %s\n", cfg.Path, backupPath)

				for name, srv := range cfg.Servers {
					if config.IsAlreadyWrapped(srv) {
						_, _ = fmt.Fprintf(out, "  %s: already wrapped, skipping\n", name)
						continue
					}

					if srv.URL != "" {
						configs[i].Servers[name] = config.RewriteForProxy("localhost:9090", name)
					} else {
						configs[i].Servers[name] = config.RewriteForWrap(srv, gremlynBinary, outputPath)
					}
				}

				if writeErr := config.WriteConfig(cfg.Path, configs[i]); writeErr != nil {
					return fmt.Errorf("writing config %s: %w", cfg.Path, writeErr)
				}
				_, _ = fmt.Fprintf(out, "Rewrote %s\n", cfg.Path)
			}

			_, _ = fmt.Fprintf(out, "\nGremlyn initialized. %d server(s) configured.\n", totalServers)
			return nil
		},
	}

	cmd.Flags().StringVar(&mcpConfigPath, "config", "", "Path to a specific MCP config file")
	cmd.Flags().StringVar(&outputPath, "output", "gremlyn.yaml", "Where to write gremlyn.yaml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be changed without changing anything")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}

func parseSingleMCPConfig(path string) (*config.MCPClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	var servers map[string]config.MCPServerEntry
	if serversRaw, ok := raw["mcpServers"]; ok {
		if err := json.Unmarshal(serversRaw, &servers); err != nil {
			return nil, err
		}
	}
	return &config.MCPClientConfig{
		Path:       path,
		ClientName: "Custom",
		Servers:    servers,
	}, nil
}
