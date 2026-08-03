package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPProxy_NormalRequestResponse(t *testing.T) {
	// Fake upstream MCP server.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Echo back a tools/list response.
		var req protocol.JSONRPCRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp := protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  json.RawMessage(`{"tools":[{"name":"search","description":"Search tool","inputSchema":{}}]}`),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer upstream.Close()

	logger := zerolog.Nop()
	cfg := Config{
		ServerName:  "test-server",
		Mode:        models.ServerModeProxy,
		UpstreamURL: upstream.URL,
		ListenAddr:  ":0",
	}

	proxy := NewHTTPProxy(cfg, WithLogger(logger))

	// Create a test request.
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/mcp/test-server", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.handleRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp protocol.JSONRPCResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "2.0", resp.JSONRPC)
}

func TestHTTPProxy_PipelineBlocks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called when request is blocked")
	}))
	defer upstream.Close()

	logger := zerolog.Nop()
	pipeline := NewPipeline(logger)

	// Register a handler that blocks everything.
	blocker := &testHandler{
		name:      "block-all",
		priority:  100,
		direction: models.DirectionBoth,
		decision:  &Decision{Action: DecisionBlock, Reason: "test block"},
	}
	pipeline.RegisterHandler(blocker)

	cfg := Config{
		ServerName:  "test",
		Mode:        models.ServerModeProxy,
		UpstreamURL: upstream.URL,
	}
	proxy := NewHTTPProxy(cfg, WithLogger(logger), WithPipeline(pipeline))

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"dangerous_tool"}}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	proxy.handleRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp protocol.JSONRPCResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotNil(t, resp.Error)
	assert.Contains(t, resp.Error.Message, "Blocked by Gremlyn")
}

func TestHTTPProxy_SSEStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flush", 500)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(200)

		events := []string{
			`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`,
			`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":"t","progress":0.5}}`,
		}
		for _, ev := range events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	logger := zerolog.Nop()
	cfg := Config{
		ServerName:  "sse-test",
		Mode:        models.ServerModeProxy,
		UpstreamURL: upstream.URL,
	}
	proxy := NewHTTPProxy(cfg, WithLogger(logger))

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()

	proxy.handleRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "data:")
}

func TestHTTPProxy_MethodNotAllowed(t *testing.T) {
	cfg := Config{
		ServerName:  "test",
		Mode:        models.ServerModeProxy,
		UpstreamURL: "http://localhost:0",
	}
	proxy := NewHTTPProxy(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()

	proxy.handleRequest(w, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestHTTPProxy_InvalidJSON(t *testing.T) {
	cfg := Config{
		ServerName:  "test",
		Mode:        models.ServerModeProxy,
		UpstreamURL: "http://localhost:0",
	}
	proxy := NewHTTPProxy(cfg, WithLogger(zerolog.Nop()))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	proxy.handleRequest(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHTTPProxy_UpstreamDown(t *testing.T) {
	cfg := Config{
		ServerName:  "test",
		Mode:        models.ServerModeProxy,
		UpstreamURL: "http://127.0.0.1:1", // Nothing listening.
	}
	proxy := NewHTTPProxy(cfg, WithLogger(zerolog.Nop()), WithTimeout(1*time.Second))

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(reqBody))
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()

	proxy.handleRequest(w, req)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestHTTPProxy_ServerName(t *testing.T) {
	cfg := Config{ServerName: "my-server", Mode: models.ServerModeProxy, UpstreamURL: "http://localhost"}
	proxy := NewHTTPProxy(cfg)
	assert.Equal(t, "my-server", proxy.ServerName())
}
