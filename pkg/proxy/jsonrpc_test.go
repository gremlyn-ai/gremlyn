package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_Parse_ToolsCallRequest_IntID(t *testing.T) {
	parser := NewParser()
	data := []byte(`{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"search","arguments":{"q":"test"}}}`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	assert.Equal(t, protocol.MessageTypeRequest, msg.Type)
	assert.Equal(t, "tools/call", msg.Request.Method)
	assert.Equal(t, int64(42), msg.Request.ID.IntVal)
	assert.False(t, msg.Request.ID.IsString)
}

func TestParser_Parse_ToolsCallRequest_StringID(t *testing.T) {
	parser := NewParser()
	data := []byte(`{"jsonrpc":"2.0","id":"req-001","method":"tools/call","params":{"name":"search"}}`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	assert.True(t, msg.Request.ID.IsString)
	assert.Equal(t, "req-001", msg.Request.ID.StringVal)
}

func TestParser_Parse_ToolsListResponse(t *testing.T) {
	parser := NewParser()
	data := []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search","description":"Search tool","inputSchema":{"type":"object"}}]}}`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	assert.Equal(t, protocol.MessageTypeResponse, msg.Type)
	assert.NotNil(t, msg.Response.Result)
	assert.Nil(t, msg.Response.Error)
}

func TestParser_Parse_Notification(t *testing.T) {
	parser := NewParser()
	data := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	assert.Equal(t, protocol.MessageTypeNotification, msg.Type)
	assert.Equal(t, "notifications/initialized", msg.Notification.Method)
}

func TestParser_Parse_NotificationWithNullID(t *testing.T) {
	parser := NewParser()
	data := []byte(`{"jsonrpc":"2.0","id":null,"method":"notifications/initialized"}`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, protocol.MessageTypeNotification, msgs[0].Type)
}

func TestParser_Parse_ErrorResponse(t *testing.T) {
	parser := NewParser()
	data := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"Invalid Request"}}`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 1)

	msg := msgs[0]
	assert.Equal(t, protocol.MessageTypeResponse, msg.Type)
	assert.NotNil(t, msg.Response.Error)
	assert.Equal(t, -32600, msg.Response.Error.Code)
}

func TestParser_Parse_Batch(t *testing.T) {
	parser := NewParser()
	data := []byte(`[
		{"jsonrpc":"2.0","id":1,"method":"tools/list"},
		{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search"}},
		{"jsonrpc":"2.0","method":"notifications/initialized"}
	]`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 3)

	assert.Equal(t, protocol.MessageTypeRequest, msgs[0].Type)
	assert.Equal(t, protocol.MessageTypeRequest, msgs[1].Type)
	assert.Equal(t, protocol.MessageTypeNotification, msgs[2].Type)
}

func TestParser_Parse_EmptyBatch(t *testing.T) {
	parser := NewParser()
	msgs, err := parser.Parse([]byte(`[]`))
	require.NoError(t, err)
	assert.Len(t, msgs, 0)
}

func TestParser_Parse_InvalidJSON(t *testing.T) {
	parser := NewParser()
	_, err := parser.Parse([]byte(`not json at all`))
	assert.Error(t, err)

	var invalidMsg *models.ErrInvalidMessage
	assert.ErrorAs(t, err, &invalidMsg)
}

func TestParser_Parse_EmptyInput(t *testing.T) {
	parser := NewParser()
	_, err := parser.Parse([]byte(``))
	assert.Error(t, err)
}

func TestParser_Parse_EmptyObject(t *testing.T) {
	parser := NewParser()
	_, err := parser.Parse([]byte(`{}`))
	assert.Error(t, err)
}

func TestParser_Parse_LargePayload(t *testing.T) {
	parser := NewParser()
	// 1MB payload in a tool response.
	largeText := strings.Repeat("a", 1024*1024)
	data := []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"` + largeText + `"}]}}`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, protocol.MessageTypeResponse, msgs[0].Type)
}

func TestParser_Serialize_RoundTrip(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name string
		msg  *protocol.Message
	}{
		{
			name: "request",
			msg: &protocol.Message{
				Type: protocol.MessageTypeRequest,
				Request: &protocol.JSONRPCRequest{
					JSONRPC: "2.0",
					ID:      protocol.NewIntID(1),
					Method:  "tools/call",
					Params:  json.RawMessage(`{"name":"test"}`),
				},
			},
		},
		{
			name: "response",
			msg: &protocol.Message{
				Type: protocol.MessageTypeResponse,
				Response: &protocol.JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      protocol.NewIntID(1),
					Result:  json.RawMessage(`{"tools":[]}`),
				},
			},
		},
		{
			name: "notification",
			msg: &protocol.Message{
				Type: protocol.MessageTypeNotification,
				Notification: &protocol.JSONRPCNotification{
					JSONRPC: "2.0",
					Method:  "notifications/initialized",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := parser.Serialize(tt.msg)
			require.NoError(t, err)
			assert.True(t, json.Valid(data))

			// Parse back.
			msgs, err := parser.Parse(data)
			require.NoError(t, err)
			require.Len(t, msgs, 1)
			assert.Equal(t, tt.msg.Type, msgs[0].Type)
		})
	}
}

func TestParser_SerializeBatch(t *testing.T) {
	parser := NewParser()
	msgs := []*protocol.Message{
		{
			Type: protocol.MessageTypeRequest,
			Request: &protocol.JSONRPCRequest{
				JSONRPC: "2.0", ID: protocol.NewIntID(1), Method: "tools/list",
			},
		},
		{
			Type: protocol.MessageTypeRequest,
			Request: &protocol.JSONRPCRequest{
				JSONRPC: "2.0", ID: protocol.NewIntID(2), Method: "tools/call",
			},
		},
	}

	data, err := parser.SerializeBatch(msgs)
	require.NoError(t, err)

	// Should be a JSON array.
	assert.True(t, data[0] == '[')

	// Parse back as batch.
	parsed, err := parser.Parse(data)
	require.NoError(t, err)
	assert.Len(t, parsed, 2)
}

func TestParser_IsBatch(t *testing.T) {
	parser := NewParser()
	assert.True(t, parser.IsBatch([]byte(`[{"jsonrpc":"2.0"}]`)))
	assert.True(t, parser.IsBatch([]byte(`  [ `)))
	assert.False(t, parser.IsBatch([]byte(`{"jsonrpc":"2.0"}`)))
	assert.False(t, parser.IsBatch([]byte(``)))
}

func TestParser_ParseToolsCallParams(t *testing.T) {
	parser := NewParser()
	raw := json.RawMessage(`{"name":"search_db","arguments":{"query":"SELECT * FROM users","limit":10}}`)

	params, err := parser.ParseToolsCallParams(raw)
	require.NoError(t, err)
	assert.Equal(t, "search_db", params.Name)
	assert.NotNil(t, params.Arguments)
}

func TestParser_ParseToolsCallResult(t *testing.T) {
	parser := NewParser()
	raw := json.RawMessage(`{"content":[{"type":"text","text":"Found 3 results"}],"isError":false}`)

	result, err := parser.ParseToolsCallResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "Found 3 results", result.Content[0].Text)
}

func TestParser_ParseToolsListResult(t *testing.T) {
	parser := NewParser()
	raw := json.RawMessage(`{"tools":[{"name":"search","description":"Search tool","inputSchema":{}}]}`)

	result, err := parser.ParseToolsListResult(raw)
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	assert.Equal(t, "search", result.Tools[0].Name)
}

func TestParser_Parse_MixedBatch(t *testing.T) {
	parser := NewParser()
	data := []byte(`[
		{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"a"}},
		{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"t","progress":0.5}},
		{"jsonrpc":"2.0","id":1,"result":{"content":[]}}
	]`)

	msgs, err := parser.Parse(data)
	require.NoError(t, err)
	require.Len(t, msgs, 3)
	assert.Equal(t, protocol.MessageTypeRequest, msgs[0].Type)
	assert.Equal(t, protocol.MessageTypeNotification, msgs[1].Type)
	assert.Equal(t, protocol.MessageTypeResponse, msgs[2].Type)
}
