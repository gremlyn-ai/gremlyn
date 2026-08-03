package policy

import (
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockResponse(t *testing.T) {
	resp := BlockResponse(protocol.NewIntID(42), "no_delete", "SQL DELETE not allowed")
	require.NotNil(t, resp)
	assert.Equal(t, protocol.MessageTypeResponse, resp.Type)
	assert.NotNil(t, resp.Response.Error)
	assert.Equal(t, -32600, resp.Response.Error.Code)
	assert.Contains(t, resp.Response.Error.Message, "Gremlyn Shield")
	assert.Contains(t, resp.Response.Error.Message, "SQL DELETE not allowed")
}

func TestRedactText(t *testing.T) {
	text := "My password is hunter2 and my secret is abc123"
	result := RedactText(text, []string{"hunter2", "abc123"})
	assert.Equal(t, "My password is [REDACTED] and my secret is [REDACTED]", result)
}

func TestRedactPII(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "email",
			input:  "Contact john@example.com for details",
			expect: "Contact [EMAIL_REDACTED] for details",
		},
		{
			name:   "phone",
			input:  "Call 555-123-4567 now",
			expect: "Call [PHONE_REDACTED] now",
		},
		{
			name:   "credit card",
			input:  "Card: 4111-1111-1111-1111",
			expect: "Card: [CC_REDACTED]",
		},
		{
			name:   "ssn",
			input:  "SSN: 123-45-6789",
			expect: "SSN: [SSN_REDACTED]",
		},
		{
			name:   "clean",
			input:  "No PII here",
			expect: "No PII here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactPII(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestPIIPatterns(t *testing.T) {
	patterns := PIIPatterns()
	assert.Contains(t, patterns, "email_address")
	assert.Contains(t, patterns, "phone_number")
	assert.Contains(t, patterns, "credit_card")
	assert.Contains(t, patterns, "ssn")
}
