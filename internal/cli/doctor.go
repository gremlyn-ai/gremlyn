package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

type checkResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// NewDoctorCmd returns the `gremlyn doctor` command, which runs the setup
// diagnostics: config validity, MCP client routing, availability of the
// configured server commands, and upstream reachability. It never fails on a
// failed check — the checks are reported as text or, with --json, as a JSON
// array — so a non-nil error means the command itself could not run.
func NewDoctorCmd(logger zerolog.Logger) *cobra.Command {
	var (
		configPath string
		jsonOutput bool
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check Gremlyn setup health",
		Long:  "Runs diagnostic checks: config validity, MCP client routing, command availability, and upstream reachability.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			out := cmd.OutOrStdout()

			var checks []checkResult

			cfg, err := config.LoadConfig(ctx, configPath)
			if err != nil {
				checks = append(checks, checkResult{
					Name:   "gremlyn.yaml",
					Status: "fail",
					Detail: err.Error(),
				})
			} else {
				checks = append(checks, checkResult{
					Name:   "gremlyn.yaml",
					Status: "ok",
					Detail: fmt.Sprintf("version %d, %d server(s)", cfg.Version, len(cfg.Servers)),
				})
			}

			mcpConfigs, mcpErr := config.DetectMCPConfigs(ctx)
			switch {
			case mcpErr != nil:
				checks = append(checks, checkResult{
					Name:   "MCP client configs",
					Status: "warn",
					Detail: fmt.Sprintf("detection error: %s", mcpErr.Error()),
				})
			case len(mcpConfigs) == 0:
				checks = append(checks, checkResult{
					Name:   "MCP client configs",
					Status: "warn",
					Detail: "no MCP client configs found",
				})
			default:
				wrappedCount := 0
				totalCount := 0
				for _, mc := range mcpConfigs {
					for _, srv := range mc.Servers {
						totalCount++
						if config.IsAlreadyWrapped(srv) {
							wrappedCount++
						}
					}
				}
				status := "ok"
				if wrappedCount < totalCount {
					status = "warn"
				}
				checks = append(checks, checkResult{
					Name:   "MCP client configs",
					Status: status,
					Detail: fmt.Sprintf("%d/%d server(s) routing through gremlyn", wrappedCount, totalCount),
				})
			}

			if cfg != nil {
				for name, srv := range cfg.Servers {
					if srv.Mode != "wrap" || srv.Command == "" {
						continue
					}
					_, lookErr := exec.LookPath(srv.Command)
					if lookErr != nil {
						checks = append(checks, checkResult{
							Name:   fmt.Sprintf("command: %s (%s)", srv.Command, name),
							Status: "fail",
							Detail: "not found on PATH",
						})
					} else {
						checks = append(checks, checkResult{
							Name:   fmt.Sprintf("command: %s (%s)", srv.Command, name),
							Status: "ok",
						})
					}
				}
			}

			if cfg != nil {
				httpClient := &http.Client{Timeout: 5 * time.Second}
				for name, srv := range cfg.Servers {
					if srv.Mode != "proxy" || srv.Upstream == "" {
						continue
					}
					req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, srv.Upstream, http.NoBody)
					if reqErr != nil {
						checks = append(checks, checkResult{
							Name:   fmt.Sprintf("upstream: %s (%s)", srv.Upstream, name),
							Status: "warn",
							Detail: fmt.Sprintf("invalid URL: %s", reqErr.Error()),
						})
						continue
					}
					resp, doErr := httpClient.Do(req)
					if doErr != nil {
						checks = append(checks, checkResult{
							Name:   fmt.Sprintf("upstream: %s (%s)", srv.Upstream, name),
							Status: "warn",
							Detail: fmt.Sprintf("unreachable: %s", doErr.Error()),
						})
					} else {
						_ = resp.Body.Close()
						checks = append(checks, checkResult{
							Name:   fmt.Sprintf("upstream: %s (%s)", srv.Upstream, name),
							Status: "ok",
							Detail: fmt.Sprintf("HTTP %d", resp.StatusCode),
						})
					}
				}
			}

			if jsonOutput {
				return json.NewEncoder(out).Encode(checks)
			}

			_, _ = fmt.Fprintln(out, "Gremlyn Doctor")
			_, _ = fmt.Fprintln(out, "==============")
			_, _ = fmt.Fprintln(out)

			hasFailures := false
			for _, c := range checks {
				var icon string
				switch c.Status {
				case "ok":
					icon = "[OK]"
				case "warn":
					icon = "[WARN]"
				case "fail":
					icon = "[FAIL]"
					hasFailures = true
				}

				if c.Detail != "" {
					_, _ = fmt.Fprintf(out, "  %s %s — %s\n", icon, c.Name, c.Detail)
				} else {
					_, _ = fmt.Fprintf(out, "  %s %s\n", icon, c.Name)
				}
			}

			_, _ = fmt.Fprintln(out)
			if hasFailures {
				_, _ = fmt.Fprintln(out, "Some checks failed. Run 'gremlyn init' to set up or fix your configuration.")
			} else {
				_, _ = fmt.Fprintln(out, "All checks passed.")
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "gremlyn.yaml", "Path to gremlyn.yaml")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}
