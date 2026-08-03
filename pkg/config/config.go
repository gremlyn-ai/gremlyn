// Package config handles parsing gremlyn.yaml configuration files and
// detecting/rewriting MCP client configurations.
package config

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"gopkg.in/yaml.v3"
)

// Config represents the top-level gremlyn.yaml configuration.
type Config struct {
	Version int                     `json:"version" yaml:"version"`
	Mode    string                  `json:"mode" yaml:"mode"`
	Servers map[string]ServerConfig `json:"servers" yaml:"servers"`
	Global  GlobalConfig            `json:"global,omitempty" yaml:"global,omitempty"`
}

// ServerConfig represents the configuration for a single MCP server.
type ServerConfig struct {
	Mode       models.ServerMode `json:"mode" yaml:"mode"`
	Command    string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args       []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Upstream   string            `json:"upstream,omitempty" yaml:"upstream,omitempty"`
	ListenAddr string            `json:"listen_addr,omitempty" yaml:"listen_addr,omitempty"`
	AuthHeader string            `json:"auth_header,omitempty" yaml:"auth_header,omitempty"` // Cloud mode: Authorization header (Bearer token, API key)
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`         // Cloud mode: additional HTTP headers
	Rules      []RuleConfig      `json:"rules" yaml:"rules"`
}

// RuleConfig represents a single rule in the configuration file.
type RuleConfig struct {
	Name          string              `json:"name" yaml:"name"`
	Match         *MatchConfig        `json:"match,omitempty" yaml:"match,omitempty"`
	ScanResponses bool                `json:"scan_responses,omitempty" yaml:"scan_responses,omitempty"`
	ScanOutgoing  bool                `json:"scan_outgoing,omitempty" yaml:"scan_outgoing,omitempty"`
	Detect        models.DetectConfig `json:"detect,omitempty" yaml:"detect,omitempty"`
	EntityField   string              `json:"entity_field,omitempty" yaml:"entity_field,omitempty"`
	Action        models.RuleAction   `json:"action" yaml:"action"`
}

// MatchConfig defines matching conditions for a rule.
// Supports dot-notation args (e.g., args.limit) via custom YAML unmarshaling.
type MatchConfig struct {
	Tool string                     `json:"tool,omitempty" yaml:"tool,omitempty"`
	Args map[string]ConditionConfig `json:"args,omitempty" yaml:"-"`
	Time *models.TimeCondition      `json:"time,omitempty" yaml:"time,omitempty"`
}

// UnmarshalYAML implements custom YAML unmarshaling for MatchConfig
// to handle dot-notation args (e.g., "args.limit: { greater_than: 100 }").
func (m *MatchConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("match config must be a mapping")
	}

	m.Args = make(map[string]ConditionConfig)

	for i := 0; i < len(value.Content)-1; i += 2 {
		key := value.Content[i].Value
		val := value.Content[i+1]

		switch {
		case key == "tool":
			m.Tool = val.Value

		case key == "time":
			var tc models.TimeCondition
			if err := val.Decode(&tc); err != nil {
				return fmt.Errorf("decoding time condition: %w", err)
			}
			m.Time = &tc

		case strings.HasPrefix(key, "args."):
			argName := strings.TrimPrefix(key, "args.")
			var cond ConditionConfig
			if err := val.Decode(&cond); err != nil {
				return fmt.Errorf("decoding condition for %q: %w", key, err)
			}
			m.Args[argName] = cond

		default:
			// Unknown keys are ignored for forward compatibility.
		}
	}

	return nil
}

// ConditionConfig defines a single condition on a tool argument value.
type ConditionConfig struct {
	GreaterThan   *float64 `json:"greater_than,omitempty" yaml:"greater_than,omitempty"`
	LessThan      *float64 `json:"less_than,omitempty" yaml:"less_than,omitempty"`
	Equals        any      `json:"equals,omitempty" yaml:"equals,omitempty"`
	MustStartWith string   `json:"must_start_with,omitempty" yaml:"must_start_with,omitempty"`
	NotContains   []string `json:"not_contains,omitempty" yaml:"not_contains,omitempty"`
	Contains      []string `json:"contains,omitempty" yaml:"contains,omitempty"`
	Regex         string   `json:"regex,omitempty" yaml:"regex,omitempty"`
}

// GlobalConfig defines global settings that apply to all servers.
type GlobalConfig struct {
	RateLimit      string               `json:"rate_limit,omitempty" yaml:"rate_limit,omitempty"`
	MaxPayloadSize string               `json:"max_payload_size,omitempty" yaml:"max_payload_size,omitempty"`
	AlertChannels  []AlertChannelConfig `json:"alert_channels,omitempty" yaml:"alert_channels,omitempty"`
}

// AlertChannelConfig defines a notification channel for alerts.
type AlertChannelConfig struct {
	Type    string `json:"type" yaml:"type"`
	Webhook string `json:"webhook,omitempty" yaml:"webhook,omitempty"`
	To      string `json:"to,omitempty" yaml:"to,omitempty"`
	URL     string `json:"url,omitempty" yaml:"url,omitempty"`
}

// LoadConfig reads and parses a gremlyn.yaml configuration file.
func LoadConfig(_ context.Context, path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %q: %w", path, err)
	}

	// Expand env vars in alert channel webhooks.
	for i := range cfg.Global.AlertChannels {
		cfg.Global.AlertChannels[i].Webhook = ExpandEnvVars(cfg.Global.AlertChannels[i].Webhook)
		cfg.Global.AlertChannels[i].URL = ExpandEnvVars(cfg.Global.AlertChannels[i].URL)
	}

	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

// ValidateConfig checks that a Config is valid.
func ValidateConfig(cfg *Config) error {
	if cfg.Version != 1 {
		return fmt.Errorf("unsupported config version: %d (expected 1)", cfg.Version)
	}

	if cfg.Mode != "" && cfg.Mode != "shield" && cfg.Mode != "arena" && cfg.Mode != "both" {
		return fmt.Errorf("invalid mode: %q (expected shield, arena, or both)", cfg.Mode)
	}

	for name, srv := range cfg.Servers {
		if !srv.Mode.Valid() {
			return fmt.Errorf("server %q: invalid mode %q", name, srv.Mode)
		}

		// Command/upstream are optional in gremlyn.yaml — they are populated from
		// the MCP client config at runtime. No additional validation needed here.

		ruleNames := make(map[string]bool)
		for _, rule := range srv.Rules {
			if rule.Name == "" {
				return fmt.Errorf("server %q: rule has empty name", name)
			}
			if ruleNames[rule.Name] {
				return fmt.Errorf("server %q: duplicate rule name %q", name, rule.Name)
			}
			ruleNames[rule.Name] = true

			if !rule.Action.Valid() {
				return fmt.Errorf("server %q, rule %q: invalid action %q", name, rule.Name, rule.Action)
			}
		}
	}

	if cfg.Global.RateLimit != "" {
		if _, _, _, err := ParseRateLimit(cfg.Global.RateLimit); err != nil {
			return fmt.Errorf("invalid global rate_limit %q: %w", cfg.Global.RateLimit, err)
		}
	}

	if cfg.Global.MaxPayloadSize != "" {
		if _, err := ParsePayloadSize(cfg.Global.MaxPayloadSize); err != nil {
			return fmt.Errorf("invalid global max_payload_size %q: %w", cfg.Global.MaxPayloadSize, err)
		}
	}

	return nil
}

// envVarPattern matches ${VAR_NAME} patterns for environment variable expansion.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ExpandEnvVars expands ${VAR_NAME} patterns in a string using os.Getenv.
func ExpandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1] // Strip ${ and }
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match // Keep original if not set.
	})
}

// ParseRateLimit parses rate limit strings like "50/minute/agent" or "100/hour".
// Returns the count, window duration, and whether it's per-agent.
func ParseRateLimit(s string) (count int, window time.Duration, perAgent bool, err error) {
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return 0, 0, false, fmt.Errorf("expected format: count/window[/agent]")
	}

	count, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false, fmt.Errorf("invalid count %q: %w", parts[0], err)
	}

	switch strings.ToLower(parts[1]) {
	case "second":
		window = time.Second
	case "minute":
		window = time.Minute
	case "hour":
		window = time.Hour
	case "day":
		window = 24 * time.Hour
	default:
		return 0, 0, false, fmt.Errorf("unknown window %q (expected second/minute/hour/day)", parts[1])
	}

	if len(parts) >= 3 && strings.ToLower(parts[2]) == "agent" {
		perAgent = true
	}

	return count, window, perAgent, nil
}

// ParsePayloadSize parses human-readable size strings like "1MB", "500KB", "10GB".
func ParsePayloadSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))

	// Check longer suffixes first to avoid "B" matching before "MB".
	suffixes := []struct {
		suffix string
		mult   int64
	}{
		{"GB", 1024 * 1024 * 1024},
		{"MB", 1024 * 1024},
		{"KB", 1024},
		{"B", 1},
	}

	for _, entry := range suffixes {
		if strings.HasSuffix(s, entry.suffix) {
			numStr := strings.TrimSuffix(s, entry.suffix)
			num, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
			if err != nil {
				return 0, fmt.Errorf("invalid number in size %q: %w", s, err)
			}
			return int64(num * float64(entry.mult)), nil
		}
	}

	return 0, fmt.Errorf("unknown size format %q (expected B/KB/MB/GB suffix)", s)
}
