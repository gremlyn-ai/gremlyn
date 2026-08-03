package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// MCPClientConfig represents a detected MCP client configuration file.
type MCPClientConfig struct {
	Path       string                    `json:"path"`
	ClientName string                    `json:"client_name"`
	Servers    map[string]MCPServerEntry `json:"mcpServers"`
	// rawData preserves unknown fields for round-trip fidelity.
	rawData map[string]json.RawMessage
}

// MCPServerEntry represents a single MCP server entry in a client config file.
type MCPServerEntry struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// DetectMCPConfigs scans known paths for MCP client configuration files.
func DetectMCPConfigs(_ context.Context) ([]MCPClientConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home directory: %w", err)
	}

	candidates := []struct {
		path       string
		clientName string
	}{
		{filepath.Join(homeDir, ".claude", "claude_desktop_config.json"), "Claude Desktop"},
		{filepath.Join(homeDir, ".cursor", "mcp.json"), "Cursor"},
		{filepath.Join(".", "mcp.json"), "Project-level"},
	}

	// Custom path from env var.
	if envPath := os.Getenv("GREMLYN_MCP_CONFIG"); envPath != "" {
		candidates = append(candidates, struct {
			path       string
			clientName string
		}{envPath, "Custom (GREMLYN_MCP_CONFIG)"})
	}

	// Windows-specific: also check %APPDATA% paths.
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			candidates = append(candidates,
				struct {
					path       string
					clientName string
				}{filepath.Join(appData, "Claude", "claude_desktop_config.json"), "Claude Desktop (AppData)"},
			)
		}
	}

	configs := make([]MCPClientConfig, 0, len(candidates))
	for _, c := range candidates {
		cfg, err := parseMCPConfig(c.path, c.clientName)
		if err != nil {
			continue // Skip files that don't exist or can't be parsed.
		}
		configs = append(configs, *cfg)
	}

	return configs, nil
}

// parseMCPConfig reads and parses a single MCP client config file.
func parseMCPConfig(path, clientName string) (*MCPClientConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Parse into a raw map to preserve unknown fields.
	var rawData map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawData); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", path, err)
	}

	var servers map[string]MCPServerEntry
	if serversRaw, ok := rawData["mcpServers"]; ok {
		if err := json.Unmarshal(serversRaw, &servers); err != nil {
			return nil, fmt.Errorf("parsing mcpServers in %q: %w", path, err)
		}
	}

	if len(servers) == 0 {
		return nil, fmt.Errorf("no MCP servers found in %q", path)
	}

	return &MCPClientConfig{
		Path:       path,
		ClientName: clientName,
		Servers:    servers,
		rawData:    rawData,
	}, nil
}

// RewriteForWrap transforms a stdio-based MCP server entry to route through gremlyn wrap.
func RewriteForWrap(entry MCPServerEntry, gremlynBinaryPath, configPath string) MCPServerEntry {
	newArgs := []string{"wrap", "--config", configPath, "--"}

	// Preserve original command as first arg after --.
	newArgs = append(newArgs, entry.Command)
	newArgs = append(newArgs, entry.Args...)

	return MCPServerEntry{
		Command: gremlynBinaryPath,
		Args:    newArgs,
		Env:     entry.Env,
	}
}

// RewriteForProxy transforms a URL-based MCP server entry to point to the gremlyn proxy.
func RewriteForProxy(proxyListenAddr, serverName string) MCPServerEntry {
	return MCPServerEntry{
		URL: fmt.Sprintf("http://%s/mcp/%s", proxyListenAddr, serverName),
	}
}

// IsAlreadyWrapped returns true if the server entry is already routing through gremlyn.
func IsAlreadyWrapped(entry MCPServerEntry) bool {
	if entry.Command == "gremlyn" {
		return true
	}
	// Also check if the command ends with /gremlyn or \gremlyn.
	base := filepath.Base(entry.Command)
	return base == "gremlyn" || base == "gremlyn.exe"
}

// BackupConfig creates a timestamped backup of the config file.
func BackupConfig(path string) (string, error) {
	backupPath := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file for backup: %w", err)
	}
	if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}
	return backupPath, nil
}

// WriteConfig writes the MCP client config back to disk, preserving unknown fields.
func WriteConfig(path string, cfg MCPClientConfig) error {
	// Start with original raw data to preserve unknown fields.
	output := make(map[string]json.RawMessage)
	for k, v := range cfg.rawData {
		output[k] = v
	}

	// Update the mcpServers field.
	serversJSON, err := json.Marshal(cfg.Servers)
	if err != nil {
		return fmt.Errorf("marshaling servers: %w", err)
	}
	output["mcpServers"] = serversJSON

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// GenerateDefaultGremlynYAML creates a default gremlyn.yaml from detected MCP configs.
func GenerateDefaultGremlynYAML(configs []MCPClientConfig) *Config {
	servers := make(map[string]ServerConfig)

	for _, cfg := range configs {
		for name, entry := range cfg.Servers {
			var sc ServerConfig
			if entry.URL != "" {
				sc.Mode = "proxy"
				sc.Upstream = entry.URL
			} else {
				sc.Mode = "wrap"
				sc.Command = entry.Command
				sc.Args = entry.Args
				sc.Env = entry.Env
			}
			sc.Rules = DefaultRules()
			servers[name] = sc
		}
	}

	return &Config{
		Version: 1,
		Mode:    "shield",
		Servers: servers,
		Global:  DefaultGlobalConfig(),
	}
}
