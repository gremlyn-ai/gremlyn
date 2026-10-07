package gremlins

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductionGremlins_CRMAgentFakeDeleteAll(t *testing.T) {
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

	assert.Equal(t, "company:Acme Corp", params.Arguments["query"])
}

func TestProductionGremlins_DatabaseResponseTruncated(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeTruncated, 1.0)

	dbResponse := `{"rows":[{"id":1,"name":"Product A","price":29.99,"category":"electronics"},{"id":2,"name":"Product B","price":49.99,"category":"home"},{"id":3,"name":"Product C","price":15.00,"category":"books"}],"total_count":3}`
	msg := makeResponseMsg(dbResponse)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	assert.Less(t, len(modified.Response.Result), len(msg.Response.Result))
}

func TestProductionGremlins_APIMissingCriticalFields(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeMissingFields, 1.0)

	paymentResponse := `{"transaction_id":"txn_abc123","amount":149.99,"currency":"USD","status":"completed","customer_id":"cust_456","receipt_url":"https://pay.example.com/receipt/abc123"}`
	msg := makeResponseMsg(paymentResponse)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(modified.Response.Result, &obj))
}

func TestProductionGremlins_WrongTypesFinancialData(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeWrongTypes, 1.0)

	financialData := `{"balance":10523.45,"transactions":15,"account":"checking"}`
	msg := makeResponseMsg(financialData)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	var obj map[string]interface{}
	require.NoError(t, json.Unmarshal(modified.Response.Result, &obj))

	assert.Contains(t, obj, "error_type")
}

func TestProductionGremlins_AgentBreaksRetryLoop(t *testing.T) {
	g := NewLoopGremlin(5, "Operation failed, please retry the request", 1.0)

	msg := makeResponseMsg(`{"status":"success","data":{"id":"ord-789","total":259.99}}`)

	for i := 0; i < 5; i++ {
		modified, injected, err := g.Inject(context.Background(), msg)
		require.NoError(t, err)
		assert.True(t, injected, "retry %d should inject", i+1)

		var result map[string]string
		require.NoError(t, json.Unmarshal(modified.Response.Result, &result))
		assert.Equal(t, "needs_verification", result["status"])
	}

	_, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, injected, "should stop injecting after max loops")
}

func TestProductionGremlins_LongLatencyPayment(t *testing.T) {
	g := NewLatencyGremlin(1, 3, 1.0)

	paymentResponse := `{"status":"approved","authorization_code":"AUTH_789","amount":500.00}`
	msg := makeResponseMsg(paymentResponse)

	modified, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, injected)

	assert.JSONEq(t, paymentResponse, string(modified.Response.Result))
}

func TestProductionGremlins_ContextCancellationStopsLatency(t *testing.T) {
	g := NewLatencyGremlin(10000, 10000, 1.0)

	msg := makeResponseMsg(`{"data":"sensitive_payment_info"}`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, injected, err := g.Inject(ctx, msg)
	assert.Error(t, err, "cancelled context should produce error")
	assert.False(t, injected)
}

func TestProductionGremlins_HallucinationOnResponseIsNoop(t *testing.T) {
	g := NewHallucinationGremlin("evil_tool", 1.0)

	msg := makeResponseMsg(`{"contact_id":"c-1234","name":"Jane Smith","company":"Acme Corp"}`)

	_, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, injected, "hallucination should not inject into responses")
}

func TestProductionGremlins_CorruptionOnToolCallIsNoop(t *testing.T) {
	g := NewCorruptionGremlin(CorruptionModeTruncated, 1.0)

	msg := makeToolCallMsg("query", map[string]string{
		"sql": "SELECT * FROM products LIMIT 10",
	})

	_, injected, err := g.Inject(context.Background(), msg)
	require.NoError(t, err)
	assert.False(t, injected, "corruption should not inject into tool calls")
}
