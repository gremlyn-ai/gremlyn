package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const exampleYAML = `
version: 1
mode: shield

servers:
  hubspot:
    mode: wrap
    rules:
      - name: "No cross-entity data leak"
        scan_responses: true
        detect: cross_entity_leak
        action: redact_and_alert

      - name: "Block bulk data exports"
        match:
          tool: "search_contacts"
          args.limit: { greater_than: 100 }
        action: block

  postgres:
    mode: proxy
    upstream: "http://localhost:5433/mcp"
    rules:
      - name: "SELECT only"
        match:
          tool: "query"
          args.sql: { must_start_with: "SELECT" }
        action: block

      - name: "No sensitive columns"
        match:
          tool: "query"
          args.sql: { not_contains: ["password", "ssn", "card_number"] }
        action: block_and_alert

  slack:
    mode: wrap
    rules:
      - name: "No PII in messages"
        scan_outgoing: true
        detect:
          - email_address
          - phone_number
          - credit_card
        action: redact

global:
  rate_limit: 50/minute/agent
  max_payload_size: 1MB
  alert_channels:
    - type: slack
      webhook: "${SLACK_WEBHOOK_URL}"
    - type: email
      to: "security@company.com"
`

func TestLoadConfig_ExampleYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gremlyn.yaml")
	require.NoError(t, os.WriteFile(path, []byte(exampleYAML), 0o644))

	cfg, err := LoadConfig(context.Background(), path)
	require.NoError(t, err)
	assert.Equal(t, 1, cfg.Version)
	assert.Equal(t, "shield", cfg.Mode)
	assert.Len(t, cfg.Servers, 3)

	// Hubspot server.
	hubspot := cfg.Servers["hubspot"]
	assert.Equal(t, models.ServerModeWrap, hubspot.Mode)
	require.Len(t, hubspot.Rules, 2)
	assert.Equal(t, "No cross-entity data leak", hubspot.Rules[0].Name)
	assert.True(t, hubspot.Rules[0].ScanResponses)
	assert.Equal(t, []string{"cross_entity_leak"}, hubspot.Rules[0].Detect.Values)
	assert.Equal(t, models.RuleActionRedactAndAlert, hubspot.Rules[0].Action)

	// Bulk export rule with dot-notation args.
	bulkRule := hubspot.Rules[1]
	assert.Equal(t, "search_contacts", bulkRule.Match.Tool)
	require.Contains(t, bulkRule.Match.Args, "limit")
	gt := bulkRule.Match.Args["limit"].GreaterThan
	require.NotNil(t, gt)
	assert.Equal(t, 100.0, *gt)

	// Postgres server.
	postgres := cfg.Servers["postgres"]
	assert.Equal(t, models.ServerModeProxy, postgres.Mode)
	assert.Equal(t, "http://localhost:5433/mcp", postgres.Upstream)
	require.Len(t, postgres.Rules, 2)

	selectRule := postgres.Rules[0]
	assert.Equal(t, "SELECT", selectRule.Match.Args["sql"].MustStartWith)

	sensitiveRule := postgres.Rules[1]
	assert.Equal(t, []string{"password", "ssn", "card_number"}, sensitiveRule.Match.Args["sql"].NotContains)

	// Slack server with list detect.
	slack := cfg.Servers["slack"]
	assert.True(t, slack.Rules[0].ScanOutgoing)
	assert.Equal(t, []string{"email_address", "phone_number", "credit_card"}, slack.Rules[0].Detect.Values)

	// Global config.
	assert.Equal(t, "50/minute/agent", cfg.Global.RateLimit)
	assert.Equal(t, "1MB", cfg.Global.MaxPayloadSize)
	assert.Len(t, cfg.Global.AlertChannels, 2)
}

func TestValidateConfig_Valid(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Mode:    "shield",
		Servers: map[string]ServerConfig{
			"test": {
				Mode:    models.ServerModeWrap,
				Command: "npx",
				Rules: []RuleConfig{
					{Name: "rule1", Action: models.RuleActionAllow},
				},
			},
		},
	}
	err := ValidateConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateConfig_WrongVersion(t *testing.T) {
	cfg := &Config{Version: 2}
	err := ValidateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestValidateConfig_WrapWithoutCommand_OK(t *testing.T) {
	// Command is optional in gremlyn.yaml — populated from MCP client config at runtime.
	cfg := &Config{
		Version: 1,
		Servers: map[string]ServerConfig{
			"test": {Mode: models.ServerModeWrap},
		},
	}
	err := ValidateConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateConfig_ProxyWithoutUpstream_OK(t *testing.T) {
	// Upstream is optional in gremlyn.yaml — populated from MCP client config at runtime.
	cfg := &Config{
		Version: 1,
		Servers: map[string]ServerConfig{
			"test": {Mode: models.ServerModeProxy},
		},
	}
	err := ValidateConfig(cfg)
	assert.NoError(t, err)
}

func TestValidateConfig_InvalidAction(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Servers: map[string]ServerConfig{
			"test": {
				Mode:    models.ServerModeWrap,
				Command: "echo",
				Rules: []RuleConfig{
					{Name: "bad", Action: "nope"},
				},
			},
		},
	}
	err := ValidateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "action")
}

func TestValidateConfig_DuplicateRuleName(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Servers: map[string]ServerConfig{
			"test": {
				Mode:    models.ServerModeWrap,
				Command: "echo",
				Rules: []RuleConfig{
					{Name: "dup", Action: models.RuleActionAllow},
					{Name: "dup", Action: models.RuleActionBlock},
				},
			},
		},
	}
	err := ValidateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestExpandEnvVars(t *testing.T) {
	t.Setenv("TEST_GREMLYN_VAR", "hello_world")

	assert.Equal(t, "hello_world", ExpandEnvVars("${TEST_GREMLYN_VAR}"))
	assert.Equal(t, "prefix-hello_world-suffix", ExpandEnvVars("prefix-${TEST_GREMLYN_VAR}-suffix"))
	assert.Equal(t, "${UNSET_VAR}", ExpandEnvVars("${UNSET_VAR}"))
	assert.Equal(t, "no vars here", ExpandEnvVars("no vars here"))
}

func TestParseRateLimit(t *testing.T) {
	tests := []struct {
		input    string
		count    int
		window   time.Duration
		perAgent bool
		wantErr  bool
	}{
		{"50/minute/agent", 50, time.Minute, true, false},
		{"100/hour", 100, time.Hour, false, false},
		{"10/second", 10, time.Second, false, false},
		{"1000/day/agent", 1000, 24 * time.Hour, true, false},
		{"invalid", 0, 0, false, true},
		{"abc/minute", 0, 0, false, true},
		{"50/unknown", 0, 0, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			count, window, perAgent, err := ParseRateLimit(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.count, count)
			assert.Equal(t, tt.window, window)
			assert.Equal(t, tt.perAgent, perAgent)
		})
	}
}

func TestParsePayloadSize(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"1MB", 1024 * 1024, false},
		{"500KB", 500 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"100B", 100, false},
		{"10mb", 10 * 1024 * 1024, false},
		{"nope", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParsePayloadSize(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
