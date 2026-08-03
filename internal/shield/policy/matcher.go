// Package policy implements the Gremlyn Shield policy engine that evaluates
// security rules against intercepted MCP messages.
package policy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/config"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// MatchResult describes whether a rule matched a message and why.
type MatchResult struct {
	Matched bool   `json:"matched"`
	Reason  string `json:"reason,omitempty"`
}

// MatchRule evaluates whether a message matches the given rule configuration.
func MatchRule(msg *protocol.Message, rule *config.RuleConfig) MatchResult {
	if rule.Match == nil {
		// Rules without match conditions match everything (used for scan-based rules).
		return MatchResult{Matched: true, Reason: "no match conditions (scan rule)"}
	}

	return matchConfig(msg, rule.Match)
}

func matchConfig(msg *protocol.Message, mc *config.MatchConfig) MatchResult {
	toolName, toolArgs := extractToolInfo(msg)

	// Check tool name match.
	if mc.Tool != "" {
		if toolName == "" {
			return MatchResult{Matched: false, Reason: "message has no tool name"}
		}
		if !matchTool(mc.Tool, toolName) {
			return MatchResult{Matched: false, Reason: fmt.Sprintf("tool %q does not match %q", toolName, mc.Tool)}
		}
	}

	// Check argument conditions.
	for argName, condition := range mc.Args {
		argValue, ok := toolArgs[argName]
		if !ok {
			return MatchResult{Matched: false, Reason: fmt.Sprintf("arg %q not present", argName)}
		}
		if !matchCondition(argValue, condition) {
			return MatchResult{Matched: false, Reason: fmt.Sprintf("arg %q condition not met", argName)}
		}
	}

	return MatchResult{Matched: true, Reason: fmt.Sprintf("tool=%q matched all conditions", toolName)}
}

func matchTool(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	// Support glob-like wildcard at end: "query*" matches "query_users".
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
	}
	return strings.EqualFold(pattern, name)
}

func matchCondition(value json.RawMessage, cond config.ConditionConfig) bool {
	strVal := strings.Trim(string(value), `"`)

	if cond.MustStartWith != "" {
		if !strings.HasPrefix(strVal, cond.MustStartWith) {
			return true // Violation: doesn't start with required prefix → rule triggers.
		}
		return false
	}

	if len(cond.NotContains) > 0 {
		lower := strings.ToLower(strVal)
		for _, forbidden := range cond.NotContains {
			if strings.Contains(lower, strings.ToLower(forbidden)) {
				return true // Violation: contains forbidden substring → rule triggers.
			}
		}
		return false
	}

	if len(cond.Contains) > 0 {
		lower := strings.ToLower(strVal)
		for _, required := range cond.Contains {
			if strings.Contains(lower, strings.ToLower(required)) {
				return true
			}
		}
		return false
	}

	if cond.GreaterThan != nil {
		var numVal float64
		if err := json.Unmarshal(value, &numVal); err != nil {
			return false
		}
		return numVal > *cond.GreaterThan
	}

	if cond.LessThan != nil {
		var numVal float64
		if err := json.Unmarshal(value, &numVal); err != nil {
			return false
		}
		return numVal < *cond.LessThan
	}

	if cond.Equals != nil {
		var raw any
		if err := json.Unmarshal(value, &raw); err != nil {
			return false
		}
		return fmt.Sprintf("%v", raw) == fmt.Sprintf("%v", cond.Equals)
	}

	if cond.Regex != "" {
		re, err := regexp.Compile(cond.Regex)
		if err != nil {
			return false
		}
		return re.MatchString(strVal)
	}

	return false
}

func extractToolInfo(msg *protocol.Message) (toolName string, args map[string]json.RawMessage) {
	if msg == nil || msg.Request == nil {
		return "", nil
	}

	method := msg.Request.Method
	if method != "tools/call" {
		return "", nil
	}

	if msg.Request.Params == nil {
		return "", nil
	}

	var params struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(msg.Request.Params, &params); err != nil {
		return "", nil
	}

	return params.Name, params.Arguments
}

// ExtractResponseText extracts all text content from a tool response message
// for scanning by the detection engine.
func ExtractResponseText(msg *protocol.Message) map[string]string {
	fields := make(map[string]string)

	if msg == nil {
		return fields
	}

	if msg.Response != nil && msg.Response.Result != nil {
		extractJSONFields("result", msg.Response.Result, fields)
	}

	return fields
}

func extractJSONFields(prefix string, data json.RawMessage, out map[string]string) {
	// Try as object.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err == nil {
		for k, v := range obj {
			key := prefix + "." + k
			// Try as string first.
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				out[key] = s
			} else {
				extractJSONFields(key, v, out)
			}
		}
		return
	}

	// Try as array.
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err == nil {
		for i, v := range arr {
			extractJSONFields(fmt.Sprintf("%s[%d]", prefix, i), v, out)
		}
		return
	}

	// Try as bare string.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		out[prefix] = s
	}
}

// RuleActionToDecision converts a models.RuleAction to the appropriate pipeline
// DecisionAction and determines if an alert should be raised.
func RuleActionToDecision(action models.RuleAction) (decision string, shouldAlert bool) {
	switch action {
	case models.RuleActionBlock:
		return "block", false
	case models.RuleActionBlockAndAlert:
		return "block", true
	case models.RuleActionRedact:
		return "redact", false
	case models.RuleActionRedactAndAlert:
		return "redact", true
	case models.RuleActionThrottle:
		return "block", false // Throttle acts as block when rate exceeded.
	case models.RuleActionLogOnly:
		return "allow", false
	case models.RuleActionAllow:
		return "allow", false
	default:
		return "allow", false
	}
}
