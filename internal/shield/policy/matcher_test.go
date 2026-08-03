package policy

import (
	"encoding/json"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/stretchr/testify/assert"
)

func makeToolCallMsg(tool string, args map[string]any) *protocol.Message {
	params := map[string]any{
		"name":      tool,
		"arguments": args,
	}
	paramsJSON, _ := json.Marshal(params)
	return &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(1),
			Method:  "tools/call",
			Params:  paramsJSON,
		},
	}
}

func TestMatchRule_ToolName(t *testing.T) {
	rule := &config.RuleConfig{
		Name:   "test",
		Match:  &config.MatchConfig{Tool: "search_contacts"},
		Action: models.RuleActionBlock,
	}

	msg := makeToolCallMsg("search_contacts", nil)
	result := MatchRule(msg, rule)
	assert.True(t, result.Matched)

	msg2 := makeToolCallMsg("send_email", nil)
	result2 := MatchRule(msg2, rule)
	assert.False(t, result2.Matched)
}

func TestMatchRule_ToolWildcard(t *testing.T) {
	rule := &config.RuleConfig{
		Name:   "test",
		Match:  &config.MatchConfig{Tool: "*"},
		Action: models.RuleActionBlock,
	}

	msg := makeToolCallMsg("anything", nil)
	result := MatchRule(msg, rule)
	assert.True(t, result.Matched)
}

func TestMatchRule_ToolPrefix(t *testing.T) {
	rule := &config.RuleConfig{
		Name:   "test",
		Match:  &config.MatchConfig{Tool: "query*"},
		Action: models.RuleActionBlock,
	}

	msg := makeToolCallMsg("query_users", nil)
	assert.True(t, MatchRule(msg, rule).Matched)

	msg2 := makeToolCallMsg("send_email", nil)
	assert.False(t, MatchRule(msg2, rule).Matched)
}

func TestMatchRule_GreaterThan(t *testing.T) {
	gt := float64(100)
	rule := &config.RuleConfig{
		Name: "limit check",
		Match: &config.MatchConfig{
			Tool: "search_contacts",
			Args: map[string]config.ConditionConfig{
				"limit": {GreaterThan: &gt},
			},
		},
		Action: models.RuleActionBlock,
	}

	msg := makeToolCallMsg("search_contacts", map[string]any{"limit": 150})
	assert.True(t, MatchRule(msg, rule).Matched)

	msg2 := makeToolCallMsg("search_contacts", map[string]any{"limit": 50})
	assert.False(t, MatchRule(msg2, rule).Matched)
}

func TestMatchRule_MustStartWith(t *testing.T) {
	rule := &config.RuleConfig{
		Name: "SELECT only",
		Match: &config.MatchConfig{
			Tool: "query",
			Args: map[string]config.ConditionConfig{
				"sql": {MustStartWith: "SELECT"},
			},
		},
		Action: models.RuleActionBlock,
	}

	// Violation: doesn't start with SELECT → triggers block.
	msg := makeToolCallMsg("query", map[string]any{"sql": "DROP TABLE users"})
	assert.True(t, MatchRule(msg, rule).Matched)

	// OK: starts with SELECT.
	msg2 := makeToolCallMsg("query", map[string]any{"sql": "SELECT * FROM users"})
	assert.False(t, MatchRule(msg2, rule).Matched)
}

func TestMatchRule_NotContains(t *testing.T) {
	rule := &config.RuleConfig{
		Name: "no sensitive columns",
		Match: &config.MatchConfig{
			Tool: "query",
			Args: map[string]config.ConditionConfig{
				"sql": {NotContains: []string{"password", "ssn"}},
			},
		},
		Action: models.RuleActionBlock,
	}

	// Violation: contains "password".
	msg := makeToolCallMsg("query", map[string]any{"sql": "SELECT password FROM users"})
	assert.True(t, MatchRule(msg, rule).Matched)

	// OK: no forbidden words.
	msg2 := makeToolCallMsg("query", map[string]any{"sql": "SELECT name FROM users"})
	assert.False(t, MatchRule(msg2, rule).Matched)
}

func TestMatchRule_NoMatchConfig(t *testing.T) {
	rule := &config.RuleConfig{
		Name:          "scan rule",
		ScanResponses: true,
		Action:        models.RuleActionBlock,
	}

	msg := makeToolCallMsg("anything", nil)
	result := MatchRule(msg, rule)
	assert.True(t, result.Matched, "rules without match conditions should match everything")
}

func TestMatchRule_NonToolCallMessage(t *testing.T) {
	rule := &config.RuleConfig{
		Name:   "test",
		Match:  &config.MatchConfig{Tool: "search"},
		Action: models.RuleActionBlock,
	}

	msg := &protocol.Message{
		Type: protocol.MessageTypeNotification,
		Notification: &protocol.JSONRPCNotification{
			JSONRPC: protocol.JSONRPCVersion,
			Method:  "notifications/initialized",
		},
	}
	result := MatchRule(msg, rule)
	assert.False(t, result.Matched)
}

func TestExtractResponseText(t *testing.T) {
	resp := &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(1),
			Result:  json.RawMessage(`{"text":"hello","nested":{"note":"[SYSTEM] hack"}}`),
		},
	}

	fields := ExtractResponseText(resp)
	assert.Equal(t, "hello", fields["result.text"])
	assert.Equal(t, "[SYSTEM] hack", fields["result.nested.note"])
}

func TestExtractResponseText_Nil(t *testing.T) {
	fields := ExtractResponseText(nil)
	assert.Empty(t, fields)
}

func TestRuleActionToDecision(t *testing.T) {
	tests := []struct {
		action    models.RuleAction
		wantDec   string
		wantAlert bool
	}{
		{models.RuleActionBlock, "block", false},
		{models.RuleActionBlockAndAlert, "block", true},
		{models.RuleActionRedact, "redact", false},
		{models.RuleActionRedactAndAlert, "redact", true},
		{models.RuleActionAllow, "allow", false},
		{models.RuleActionLogOnly, "allow", false},
		{models.RuleActionThrottle, "block", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			dec, alert := RuleActionToDecision(tt.action)
			assert.Equal(t, tt.wantDec, dec)
			assert.Equal(t, tt.wantAlert, alert)
		})
	}
}
