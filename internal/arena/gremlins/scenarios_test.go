package gremlins

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Production scenario: CRM agent receives fake delete_all tool
// ---------------------------------------------------------------------------

func TestProductionGremlins_CRMAgentFakeDeleteAll(t *testing.T) {
	// A CRM agent calls search_contacts. The hallucination gremlin replaces
	// the tool name with "delete_all_contacts" — simulating a hallucinated tool.
	g := NewHallucinationGremlin("delete_all_contacts", 1.0)

	msg := makeToolCallMsg("search_contacts", map[string]string{
		"query": "company:Acme Corp",
		"limit": "20",
	})

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	var params struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments"`
	}
	require.NoError(t, json.Unmarshal(modified.Request.Params, &params))
	assert.Equal(t, "delete_all_contacts", params.Name)
	// Original args should be preserved.
	assert.Equal(t, "company:Acme Corp", params.Arguments["query"])
}

// ---------------------------------------------------------------------------
// Production scenario: Database agent response truncated mid-JSON
// ---------------------------------------------------------------------------

func TestProductionGremlins_DatabaseResponseTruncated(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeTruncated, 1.0)

	// Realistic DB response with multiple rows.
	dbResponse := `{"rows":[{"id":1,"name":"Product A","price":29.99,"category":"electronics"},{"id":2,"name":"Product B","price":49.99,"category":"home"},{"id":3,"name":"Product C","price":15.00,"category":"books"}],"total_count":3}`
	msg := makeResponseMsg(dbResponse)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	// Truncated response should be shorter.
	assert.Less(t, len(modified.Response.Result), len(msg.Response.Result))
}

// ---------------------------------------------------------------------------
// Production scenario: API response with missing critical fields
// ---------------------------------------------------------------------------

func TestProductionGremlins_APIMissingCriticalFields(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeMissingFields, 1.0)

	// Payment API response — missing fields could cause financial issues.
	paymentResponse := `{"transaction_id":"txn_abc123","amount":149.99,"currency":"USD","status":"completed","customer_id":"cust_456","receipt_url":"https://pay.example.com/receipt/abc123"}`
	msg := makeResponseMsg(paymentResponse)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	// Result should still be valid JSON.
	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(modified.Response.Result, &obj))
}

// ---------------------------------------------------------------------------
// Production scenario: Wrong types in financial data
// ---------------------------------------------------------------------------

func TestProductionGremlins_WrongTypesFinancialData(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeWrongTypes, 1.0)

	financialData := `{"balance":10523.45,"transactions":15,"account":"checking"}`
	msg := makeResponseMsg(financialData)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(modified.Response.Result, &obj))
	// The corruption should have introduced wrong types.
	assert.Contains(t, obj, "error_type")
}

// ---------------------------------------------------------------------------
// Production scenario: Agent breaks retry loop within limit
// ---------------------------------------------------------------------------

func TestProductionGremlins_AgentBreaksRetryLoop(t *testing.T) {
	g := NewLoopGremlin(5, "Operation failed, please retry the request", 1.0)

	msg := makeResponseMsg(`{"status":"success","data":{"id":"ord-789","total":259.99}}`)

	// Simulate agent retrying 5 times.
	for i := 0; i < 5; i++ {
		modified, injected, err := g.Inject(context.Background(), msg)
		require.NoError(t, err)
		assert.True(t, injected, "retry %d should inject", i+1)

		var result map[string]string
		require.NoError(t, json.Unmarshal(modified.Response.Result, &result))
		assert.Equal(t, "needs_verification", result["status"])
	}

	// 6th attempt: gremlin stops, agent "breaks free".
	_, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, injected, "should stop injecting after max loops")
}

// ---------------------------------------------------------------------------
// Production scenario: Long latency on payment processing
// ---------------------------------------------------------------------------

func TestProductionGremlins_LongLatencyPayment(t *testing.T) {
	// Use very small delays for test speed.
	g := NewLatencyGremlin(1, 3, 1.0)

	paymentResponse := `{"status":"approved","authorization_code":"AUTH_789","amount":500.00}`
	msg := makeResponseMsg(paymentResponse)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	// Response content should be unmodified — only delayed.
	assert.JSONEq(t, paymentResponse, string(modified.Response.Result))
}

// ---------------------------------------------------------------------------
// Production scenario: Context cancellation stops latency gremlin
// ---------------------------------------------------------------------------

func TestProductionGremlins_ContextCancellationStopsLatency(t *testing.T) {
	// 10-second delay that should be cancelled immediately.
	g := NewLatencyGremlin(10000, 10000, 1.0)

	msg := makeResponseMsg(`{"data":"sensitive_payment_info"}`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, injected, err := g.Inject(ctx, msg)
	assert.Error(t, err, "cancelled context should produce error")
	assert.False(t, injected)
}

// ---------------------------------------------------------------------------
// Production scenario: Full gremlin registry with all types
// ---------------------------------------------------------------------------

func TestProductionGremlins_FullRegistrySetup(t *testing.T) {
	r := NewRegistry()
	r.Register(NewHallucinationGremlin("nonexistent_tool", 0.5))
	r.Register(NewLatencyGremlin(500, 2000, 0.3))
	r.Register(NewCorruptionGremlin(CorruptionModeMissingFields, 0.4))
	r.Register(NewLoopGremlin(3, "Please retry", 0.2))

	assert.Len(t, r.List(), 4)

	names := r.Names()
	assert.Contains(t, names, "hallucination")
	assert.Contains(t, names, "latency")
	assert.Contains(t, names, "corruption")
	assert.Contains(t, names, "loop")

	// Each gremlin should be retrievable.
	for _, name := range names {
		g, ok := r.Get(name)
		assert.True(t, ok, "gremlin %s should be in registry", name)
		assert.NotEmpty(t, g.Description())
	}
}

// ---------------------------------------------------------------------------
// Production scenario: Hallucination on non-tool-call is no-op
// ---------------------------------------------------------------------------

func TestProductionGremlins_HallucinationOnResponseIsNoop(t *testing.T) {
	g := NewHallucinationGremlin("evil_tool", 1.0)

	// CRM response — hallucination gremlin should skip responses.
	msg := makeResponseMsg(`{"contact_id":"c-1234","name":"Jane Smith","company":"Acme Corp"}`)

	_, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, injected, "hallucination should not inject into responses")
}

// ---------------------------------------------------------------------------
// Production scenario: Corruption on tool call is no-op
// ---------------------------------------------------------------------------

func TestProductionGremlins_CorruptionOnToolCallIsNoop(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeTruncated, 1.0)

	msg := makeToolCallMsg("query", map[string]string{
		"sql": "SELECT * FROM products LIMIT 10",
	})

	_, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, injected, "corruption should not inject into tool calls")
}
