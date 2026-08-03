package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentLengthFramer_RoundTrip(t *testing.T) {
	framer := ContentLengthFramer{}
	payload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search"}}`

	var buf bytes.Buffer
	err := framer.WriteFrame(&buf, []byte(payload))
	require.NoError(t, err)

	reader := bufio.NewReader(&buf)
	data, err := framer.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, payload, string(data))
}

func TestContentLengthFramer_MultipleMessages(t *testing.T) {
	framer := ContentLengthFramer{}
	messages := []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`,
	}

	var buf bytes.Buffer
	for _, msg := range messages {
		err := framer.WriteFrame(&buf, []byte(msg))
		require.NoError(t, err)
	}

	reader := bufio.NewReader(&buf)
	for _, expected := range messages {
		data, err := framer.ReadFrame(reader)
		require.NoError(t, err)
		assert.Equal(t, expected, string(data))
	}
}

func TestContentLengthFramer_NonASCII(t *testing.T) {
	framer := ContentLengthFramer{}
	payload := `{"jsonrpc":"2.0","id":1,"result":{"text":"日本語テスト 🎉"}}`

	var buf bytes.Buffer
	err := framer.WriteFrame(&buf, []byte(payload))
	require.NoError(t, err)

	reader := bufio.NewReader(&buf)
	data, err := framer.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, payload, string(data))
}

func TestContentLengthFramer_MissingHeader(t *testing.T) {
	framer := ContentLengthFramer{}
	reader := bufio.NewReader(bytes.NewReader([]byte("Bad-Header: 10\r\n\r\n{}")))
	_, err := framer.ReadFrame(reader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing Content-Length")
}

func TestContentLengthFramer_IncompleteBody(t *testing.T) {
	framer := ContentLengthFramer{}
	// Content-Length says 100 but only 5 bytes of body.
	reader := bufio.NewReader(bytes.NewReader([]byte("Content-Length: 100\r\n\r\nhello")))
	_, err := framer.ReadFrame(reader)
	assert.Error(t, err)
}

func TestStdioTransport_RoundTrip(t *testing.T) {
	// Use io.Pipe to simulate stdio.
	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()

	clientTransport := NewStdioTransport(clientRead, clientWrite)
	serverTransport := NewStdioTransport(serverRead, serverWrite)

	ctx := context.Background()

	// Client sends a request.
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

	// Server reads the request.
	received, err := serverTransport.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeRequest, received.Type)
	assert.Equal(t, "tools/list", received.Request.Method)
	assert.Equal(t, int64(1), received.Request.ID.IntVal)

	// Server sends a response.
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

	// Client reads the response.
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

			// Verify round-trip.
			parsed, err := parseRawMessage(data)
			require.NoError(t, err)
			assert.Equal(t, tt.msg.Type, parsed.Type)
		})
	}
}

func TestHTTPTransport_JSONResponse(t *testing.T) {
	// Create a fake upstream server that returns a JSON response.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`
		_, _ = w.Write([]byte(resp))
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, server.Client())
	defer func() { _ = transport.Close() }()

	ctx := context.Background()

	msg := &Message{
		Type: MessageTypeRequest,
		Request: &JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      NewIntID(1),
			Method:  "tools/list",
		},
	}

	err := transport.WriteMessage(ctx, msg)
	require.NoError(t, err)

	received, err := transport.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, received.Type)
	assert.Equal(t, int64(1), received.Response.ID.IntVal)
}

func TestHTTPTransport_SSEResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		events := []string{
			`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search","description":"Search tool"}]}}`,
			`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"tok","progress":0.5}}`,
		}

		for _, event := range events {
			_, _ = w.Write([]byte("data: " + event + "\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	transport := NewHTTPTransport(server.URL, server.Client())
	defer func() { _ = transport.Close() }()

	ctx := context.Background()

	msg := &Message{
		Type: MessageTypeRequest,
		Request: &JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      NewIntID(1),
			Method:  "tools/list",
		},
	}

	err := transport.WriteMessage(ctx, msg)
	require.NoError(t, err)

	// Read first SSE event.
	received, err := transport.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeResponse, received.Type)

	// Read second SSE event.
	received, err = transport.ReadMessage(ctx)
	require.NoError(t, err)
	assert.Equal(t, MessageTypeNotification, received.Type)
	assert.Equal(t, "notifications/progress", received.Notification.Method)
}

func TestHTTPTransport_ClosedWrite(t *testing.T) {
	transport := NewHTTPTransport("http://localhost:0", nil)
	_ = transport.Close()

	msg := &Message{
		Type: MessageTypeRequest,
		Request: &JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      NewIntID(1),
			Method:  "ping",
		},
	}
	err := transport.WriteMessage(context.Background(), msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transport closed")
}
