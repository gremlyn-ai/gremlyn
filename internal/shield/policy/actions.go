package policy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

// BlockResponse creates a JSON-RPC error response for a blocked request.
func BlockResponse(requestID protocol.JSONRPCID, ruleName, reason string) *protocol.Message {
	errMsg := fmt.Sprintf("Blocked by Gremlyn Shield: %s", reason)
	return &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      requestID,
			Error: &protocol.JSONRPCError{
				Code:    -32600,
				Message: errMsg,
				Data:    json.RawMessage(fmt.Sprintf(`{"rule":%q}`, ruleName)),
			},
		},
	}
}

// RedactText replaces sensitive patterns in text with [REDACTED].
func RedactText(text string, patterns []string) string {
	result := text
	for _, p := range patterns {
		result = strings.ReplaceAll(result, p, "[REDACTED]")
	}
	return result
}

// RedactPII replaces common PII patterns in text.
func RedactPII(text string) string {
	redacted := text

	// Email addresses.
	redacted = emailRegex.ReplaceAllString(redacted, "[EMAIL_REDACTED]")

	// Phone numbers (simple pattern).
	redacted = phoneRegex.ReplaceAllString(redacted, "[PHONE_REDACTED]")

	// Credit card numbers (simple pattern).
	redacted = creditCardRegex.ReplaceAllString(redacted, "[CC_REDACTED]")

	// SSN pattern.
	redacted = ssnRegex.ReplaceAllString(redacted, "[SSN_REDACTED]")

	return redacted
}

// PIIPatterns returns the list of PII category names that RedactPII handles.
func PIIPatterns() []string {
	return []string{"email_address", "phone_number", "credit_card", "ssn"}
}
