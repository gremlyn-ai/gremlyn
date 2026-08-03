package config

import "github.com/gremlyn-ai/gremlyn/pkg/models"

// DefaultRules returns a set of sensible default security rules for new installations.
func DefaultRules() []RuleConfig {
	return []RuleConfig{
		{
			Name:          "Block prompt injection patterns",
			ScanResponses: true,
			Detect:        models.DetectConfig{Values: []string{"prompt_injection"}},
			Action:        models.RuleActionBlockAndAlert,
		},
		{
			Name:         "Detect PII in outgoing messages",
			ScanOutgoing: true,
			Detect:       models.DetectConfig{Values: []string{"email_address", "phone_number", "credit_card"}},
			Action:       models.RuleActionLogOnly,
		},
		{
			Name:   "Rate limit per agent",
			Action: models.RuleActionThrottle,
		},
		{
			Name:   "Log all traffic",
			Action: models.RuleActionLogOnly,
		},
	}
}

// DefaultGlobalConfig returns sensible global defaults.
func DefaultGlobalConfig() GlobalConfig {
	return GlobalConfig{
		RateLimit:      "100/minute/agent",
		MaxPayloadSize: "10MB",
	}
}
