package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStdioTransport_RoundTrip(t *testing.T) {
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	clientTransport := NewStdioTransport(clientRead, clientWrite)
	serverTransport := NewStdioTransport(serverRead, serverWrite)

	ctx := context.Background()

	reqMsg := &Message{
		Type: MessageTypeRequest,
		Request: &JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      NewIntID(1),
			Method:  "tools/list",
		},
	}

	go func() {
		err := clientTransport.WriteMessage(ctx, reqMsg)
		assert.NoError(t, err)
	}()

	received, err := serverTransport.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRequest, received.Type)
	assert.Equal(t, "tools/list", received.Request.Method)
	assert.Equal(t, int64(1), received.Request.ID.IntVal)

	respMsg := &Message{
		Type: MessageTypeResponse,
		Response: &JSONRPCResponse{
			JSONRPC: JSONRPCVersion,
			ID:      NewIntID(1),
			Result:  json.RawMessage(`{"tools":[]}`),
		},
	}

	go func() {
		err := serverTransport.WriteMessage(ctx, respMsg)
		assert.NoError(t, err)
	}()

	received, err = clientTransport.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, received.Type)
	assert.Equal(t, int64(1), received.Response.ID.IntVal)

	_ = clientTransport.Close()
	_ = serverTransport.Close()
}

func TestStdioTransport_ClosedRead(t *testing.T) {
	r, _ := io.Pipe()
	transport := NewStdioTransport(r, io.Discard)
	_ = transport.Close()

	_, err := transport.ReadMessage(context.Background())
	assert.ErrorIs(t, err, io.EOF)
}

func TestStdioTransport_ClosedWrite(t *testing.T) {
	transport := NewStdioTransport(bytes.NewReader(nil), io.Discard)
	_ = transport.Close()

	msg := &Message{
		Type: MessageTypeNotification,
		Notification: &JSONRPCNotification{
			JSONRPC: JSONRPCVersion,
			Method:  "notifications/initialized",
		},
	}
	err := transport.WriteMessage(context.Background(), msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transport closed")
}

func TestParseRawMessage_Request(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"test"}}`)
	msg, err := parseRawMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRequest, msg.Type)
	assert.NotNil(t, msg.Request)
	assert.Equal(t, "tools/call", msg.Request.Method)
	assert.Equal(t, int64(42), msg.Request.ID.IntVal)
}

func TestParseRawMessage_Response(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":42,"result":{"tools":[]}}`)
	msg, err := parseRawMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, msg.Type)
	assert.NotNil(t, msg.Response)
}

func TestParseRawMessage_ErrorResponse(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":42,"error":{"code":-32600,"message":"Invalid"}}`)
	msg, err := parseRawMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, msg.Type)
	assert.NotNil(t, msg.Response.Error)
}

func TestParseRawMessage_Notification(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	msg, err := parseRawMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeNotification, msg.Type)
	assert.NotNil(t, msg.Notification)
}

func TestParseRawMessage_NotificationWithNullID(t *testing.T) {
	data := []byte(`{"jsonrpc":"2.0","id":null,"method":"notifications/initialized"}`)
	msg, err := parseRawMessage(data)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeNotification, msg.Type)
}

func TestParseRawMessage_InvalidJSON(t *testing.T) {
	_, err := parseRawMessage([]byte(`not json`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestParseRawMessage_EmptyObject(t *testing.T) {
	_, err := parseRawMessage([]byte(`{}`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine")
}

func TestSerializeMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  *Message
	}{
		{
			name: "request",
			msg: &Message{
				Type: MessageTypeRequest,
				Request: &JSONRPCRequest{
					JSONRPC: JSONRPCVersion,
					ID:      NewIntID(1),
					Method:  "tools/list",
				},
			},
		},
		{
			name: "response",
			msg: &Message{
				Type: MessageTypeResponse,
				Response: &JSONRPCResponse{
					JSONRPC: JSONRPCVersion,
					ID:      NewIntID(1),
					Result:  json.RawMessage(`{}`),
				},
			},
		},
		{
			name: "notification",
			msg: &Message{
				Type: MessageTypeNotification,
				Notification: &JSONRPCNotification{
					JSONRPC: JSONRPCVersion,
					Method:  "notifications/initialized",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := serializeMessage(tt.msg)
			require.NoError(t, err)
			assert.True(t, json.Valid(data))

			parsed, err := parseRawMessage(data)
			require.NoError(t, err)
			assert.Equal(t, tt.msg.Type, parsed.Type)
		})
	}
}
