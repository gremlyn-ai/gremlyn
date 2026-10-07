package protocol

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONRPCID_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		id       JSONRPCID
		expected string
	}{
		{
			name:     "integer id",
			id:       NewIntID(42),
			expected: "42",
		},
		{
			name:     "string id",
			id:       NewStringID("req-001"),
			expected: `"req-001"`,
		},
		{
			name:     "zero integer id",
			id:       NewIntID(0),
			expected: "0",
		},
		{
			name:     "empty string id",
			id:       NewStringID(""),
			expected: `""`,
		},
		{
			name:     "unset id",
			id:       JSONRPCID{},
			expected: "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(data))

			var decoded JSONRPCID
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)

			if tt.id.IsSet() {
				assert.True(t, decoded.IsSet())
				assert.Equal(t, tt.id.IsString, decoded.IsString)
				if tt.id.IsString {
					assert.Equal(t, tt.id.StringVal, decoded.StringVal)
				} else {
					assert.Equal(t, tt.id.IntVal, decoded.IntVal)
				}
			} else {
				assert.False(t, decoded.IsSet())
			}
		})
	}
}

func TestJSONRPCID_UnmarshalInvalid(t *testing.T) {
	var id JSONRPCID
	err := json.Unmarshal([]byte(`{"key":"val"}`), &id)
	assert.Error(t, err)
}

func TestJSONRPCID_String(t *testing.T) {
	assert.Equal(t, "42", NewIntID(42).String())
	assert.Equal(t, "hello", NewStringID("hello").String())
	assert.Equal(t, "<unset>", JSONRPCID{}.String())
}

func TestJSONRPCRequest_MarshalRoundTrip(t *testing.T) {
	req := JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      NewIntID(1),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"search","arguments":{"query":"hello"}}`),
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded JSONRPCRequest
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, req.JSONRPC, decoded.JSONRPC)
	assert.Equal(t, req.Method, decoded.Method)
	assert.True(t, decoded.ID.IsSet())
	assert.Equal(t, int64(1), decoded.ID.IntVal)
}

func TestJSONRPCResponse_MarshalRoundTrip(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      NewIntID(1),
		Result:  json.RawMessage(`{"tools":[{"name":"search","description":"Search tool"}]}`),
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded JSONRPCResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, JSONRPCVersion, decoded.JSONRPC)
	assert.Nil(t, decoded.Error)
	assert.NotNil(t, decoded.Result)
}

func TestJSONRPCResponse_WithError(t *testing.T) {
	resp := JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      NewStringID("abc"),
		Error: &JSONRPCError{
			Code:    -32600,
			Message: "Invalid Request",
		},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded JSONRPCResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.NotNil(t, decoded.Error)
	assert.Equal(t, -32600, decoded.Error.Code)
	assert.Equal(t, "Invalid Request", decoded.Error.Message)
	assert.True(t, decoded.ID.IsString)
	assert.Equal(t, "abc", decoded.ID.StringVal)
}

func TestJSONRPCNotification_MarshalRoundTrip(t *testing.T) {
	notif := JSONRPCNotification{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	}

	data, err := json.Marshal(notif)
	require.NoError(t, err)

	var decoded JSONRPCNotification
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, "notifications/initialized", decoded.Method)
}

func TestMessage_GetMethod(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		expected string
	}{
		{
			name: "request method",
			msg: Message{
				Type:    MessageTypeRequest,
				Request: &JSONRPCRequest{Method: "tools/call"},
			},
			expected: "tools/call",
		},
		{
			name: "notification method",
			msg: Message{
				Type:         MessageTypeNotification,
				Notification: &JSONRPCNotification{Method: "notifications/initialized"},
			},
			expected: "notifications/initialized",
		},
		{
			name: "response has no method",
			msg: Message{
				Type:     MessageTypeResponse,
				Response: &JSONRPCResponse{},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.msg.GetMethod())
		})
	}
}

func TestMessage_GetID(t *testing.T) {
	reqMsg := Message{
		Type:    MessageTypeRequest,
		Request: &JSONRPCRequest{ID: NewIntID(5)},
	}
	id, ok := reqMsg.GetID()
	assert.True(t, ok)
	assert.Equal(t, int64(5), id.IntVal)

	respMsg := Message{
		Type:     MessageTypeResponse,
		Response: &JSONRPCResponse{ID: NewStringID("resp-1")},
	}
	id, ok = respMsg.GetID()
	assert.True(t, ok)
	assert.Equal(t, "resp-1", id.StringVal)

	notifMsg := Message{
		Type:         MessageTypeNotification,
		Notification: &JSONRPCNotification{},
	}
	_, ok = notifMsg.GetID()
	assert.False(t, ok)
}

func TestToolsCallParams_Unmarshal(t *testing.T) {
	raw := `{"name":"search_db","arguments":{"query":"SELECT * FROM users","limit":10}}`
	var params ToolsCallParams
	err := json.Unmarshal([]byte(raw), &params)
	require.NoError(t, err)
	assert.Equal(t, "search_db", params.Name)
	assert.NotNil(t, params.Arguments)
}

func TestToolsListResult_Unmarshal(t *testing.T) {
	raw := `{"tools":[{"name":"search","description":"Search for items","inputSchema":{"type":"object","properties":{"query":{"type":"string"}}}}]}`
	var result ToolsListResult
	err := json.Unmarshal([]byte(raw), &result)
	require.NoError(t, err)
	require.Len(t, result.Tools, 1)
	assert.Equal(t, "search", result.Tools[0].Name)
	assert.Equal(t, "Search for items", result.Tools[0].Description)
}

func TestToolsCallResult_Unmarshal(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"Found 3 results"}],"isError":false}`
	var result ToolsCallResult
	err := json.Unmarshal([]byte(raw), &result)
	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "Found 3 results", result.Content[0].Text)
	assert.False(t, result.IsError)
}

func TestContentBlock_ImageType(t *testing.T) {
	raw := `{"type":"image","mimeType":"image/png","data":"iVBORw0KGgo="}`
	var block ContentBlock
	err := json.Unmarshal([]byte(raw), &block)
	require.NoError(t, err)
	assert.Equal(t, "image", block.Type)
	assert.Equal(t, "image/png", block.MIMEType)
	assert.Equal(t, "iVBORw0KGgo=", block.Data)
}

func TestSamplingCreateMessageParams_Unmarshal(t *testing.T) {
	raw := `{
		"messages": [{"role": "user", "content": {"type": "text", "text": "Hello"}}],
		"maxTokens": 100,
		"systemPrompt": "You are helpful"
	}`
	var params SamplingCreateMessageParams
	err := json.Unmarshal([]byte(raw), &params)
	require.NoError(t, err)
	require.Len(t, params.Messages, 1)
	assert.Equal(t, "user", params.Messages[0].Role)
	assert.Equal(t, 100, params.MaxTokens)
	assert.Equal(t, "You are helpful", params.SystemPrompt)
}
