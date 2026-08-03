package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultRules(t *testing.T) {
	rules := DefaultRules()
	assert.NotEmpty(t, rules)

	for _, r := range rules {
		assert.NotEmpty(t, r.Name, "every default rule must have a name")
		assert.True(t, r.Action.Valid(), "rule %q has invalid action %q", r.Name, r.Action)
	}
}

func TestDefaultGlobalConfig(t *testing.T) {
	cfg := DefaultGlobalConfig()
	assert.NotEmpty(t, cfg.RateLimit)
	assert.NotEmpty(t, cfg.MaxPayloadSize)

	_, _, _, err := ParseRateLimit(cfg.RateLimit)
	assert.NoError(t, err)

	_, err = ParsePayloadSize(cfg.MaxPayloadSize)
	assert.NoError(t, err)
}
