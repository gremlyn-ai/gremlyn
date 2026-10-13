package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type funcStage struct {
	fn func(ctx context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error)
}

func (s *funcStage) Name() string                { return "func-stage" }
func (s *funcStage) Priority() int               { return 100 }
func (s *funcStage) Direction() models.Direction { return models.DirectionBoth }
func (s *funcStage) HandleMessage(ctx context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
	return s.fn(ctx, msg, mctx)
}

type seenMessage struct {
	direction  models.Direction
	method     string
	correlated string
	session    string
	kind       protocol.MessageType
}

type recorder struct {
	mu   sync.Mutex
	seen []seenMessage
}

func (r *recorder) stage() *funcStage {
	return &funcStage{fn: func(_ context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
		s := seenMessage{direction: mctx.Direction, method: msg.GetMethod(), session: mctx.Session, kind: msg.Type}
		if mctx.CorrelatedRequest != nil {
			s.correlated = mctx.CorrelatedRequest.GetMethod()
		}
		r.mu.Lock()
		r.seen = append(r.seen, s)
		r.mu.Unlock()
		return &Decision{Action: DecisionSkip}, nil
	}}
}

func (r *recorder) all() []seenMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]seenMessage(nil), r.seen...)
}

func (r *recorder) incoming() []seenMessage {
	var out []seenMessage
	for _, s := range r.all() {
		if s.direction == models.DirectionIncoming {
			out = append(out, s)
		}
	}
	return out
}

type upstreamCall struct {
	method string
	header http.Header
	body   string
}

type upstream struct {
	*httptest.Server
	mu    sync.Mutex
	calls []upstreamCall
}

func newUpstream(t *testing.T, handler http.HandlerFunc) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.calls = append(u.calls, upstreamCall{method: r.Method, header: r.Header.Clone(), body: string(body)})
		u.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(body))
		handler(w, r)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) recorded() []upstreamCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]upstreamCall(nil), u.calls...)
}

func rpcID(t *testing.T, body string) json.RawMessage {
	t.Helper()
	var m struct {
		ID json.RawMessage `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &m))
	return m.ID
}

func jsonUpstream(t *testing.T, result string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg, err := protocol.ParseMessage(body)
		if err != nil {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		if msg.Type != protocol.MessageTypeRequest {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if msg.GetMethod() == "initialize" {
			w.Header().Set("Mcp-Session-Id", "sess-1")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"jsonrpc": "2.0",  "id": %s, "result": %s}`, rpcID(t, string(body)), result)
	}
}

func sseUpstream(t *testing.T, result string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		msg, err := protocol.ParseMessage(body)
		if err != nil || msg.Type != protocol.MessageTypeRequest {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "id: e1\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{\"progress\":1}}\n\n")
		_, _ = fmt.Fprintf(w, "id: e2\ndata: {\"jsonrpc\": \"2.0\", \"id\": %s,\ndata:  \"result\": %s}\n\n", rpcID(t, string(body)), result)
	}
}

type proxyOpt func(*HTTPConfig)

func startHTTPProxy(t *testing.T, upstreamURL string, pipeline *Pipeline, logger zerolog.Logger, mods []proxyOpt, opts ...Option) *HTTPProxy {
	t.Helper()
	cfg := HTTPConfig{ServerName: "fake", URL: upstreamURL, RequestTimeout: 2 * time.Second}
	for _, m := range mods {
		m(&cfg)
	}
	if pipeline == nil {
		pipeline = NewPipeline(zerolog.Nop())
	}
	t.Cleanup(pipeline.Close)
	all := append([]Option{WithLogger(logger), WithPipeline(pipeline)}, opts...)
	p, err := NewHTTPProxy(cfg, all...)
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
	})
	return p
}

type reply struct {
	status int
	header http.Header
	body   string
}

func call(t *testing.T, method, target, body string, header map[string]string) reply {
	t.Helper()
	resp := openStream(t, method, target, body, header)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return reply{status: resp.StatusCode, header: resp.Header, body: string(b)}
}

func openStream(t *testing.T, method, target, body string, header map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, target, reader)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json, text/event-stream")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

type rpcErrorBody struct {
	ID    json.RawMessage `json:"id"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeRPCError(t *testing.T, body string) rpcErrorBody {
	t.Helper()
	var out rpcErrorBody
	require.NoError(t, json.Unmarshal([]byte(body), &out), body)
	require.NotNil(t, out.Error, body)
	return out
}

const toolCall = `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read","arguments":{}}}`

func TestHTTPProxy_NoOpIsByteExact(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*testing.T, string) http.HandlerFunc
	}{
		{"JSON response", jsonUpstream},
		{"SSE response", sseUpstream},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var upstreamBody bytes.Buffer
			var mu sync.Mutex
			inner := tt.handler(t, `{"content":[{"type":"text",  "text":"hé"}]}`)
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				rec := httptest.NewRecorder()
				inner(rec, r)
				mu.Lock()
				upstreamBody.Write(rec.Body.Bytes())
				mu.Unlock()
				for k, v := range rec.Header() {
					w.Header()[k] = v
				}
				w.WriteHeader(rec.Code)
				_, _ = w.Write(rec.Body.Bytes())
			})
			rec := &recorder{}
			pipeline := NewPipeline(zerolog.Nop())
			pipeline.RegisterHandler(rec.stage())
			p := startHTTPProxy(t, up.URL, pipeline, zerolog.Nop(), nil)

			sent := `{"jsonrpc":"2.0",  "id":7,"method":"tools/call","params":{"name":"read", "arguments":{}}}`
			resp := call(t, http.MethodPost, p.URL(), sent, nil)
			got := resp.body

			require.Len(t, up.recorded(), 1)
			assert.Equal(t, sent, up.recorded()[0].body, "the request must reach the server byte for byte")
			mu.Lock()
			assert.Equal(t, upstreamBody.String(), got, "the response must reach the agent byte for byte")
			mu.Unlock()
			assert.NotEmpty(t, rec.incoming(), "the pipeline must still see the response")
		})
	}
}

func TestHTTPProxy_SSEEventsGoThroughThePipelineOneByOne(t *testing.T) {
	up := newUpstream(t, sseUpstream(t, `{"ok":true}`))
	rec := &recorder{}
	pipeline := NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(rec.stage())
	p := startHTTPProxy(t, up.URL, pipeline, zerolog.Nop(), nil)

	resp := call(t, http.MethodPost, p.URL(), toolCall, map[string]string{"Mcp-Session-Id": "s9"})
	assert.Equal(t, "text/event-stream", resp.header.Get("Content-Type"))
	body := resp.body
	assert.Contains(t, body, "id: e1\n")
	assert.Contains(t, body, "id: e2\n")

	in := rec.incoming()
	require.Len(t, in, 2, "each event is its own message")
	assert.Equal(t, "notifications/progress", in[0].method)
	assert.Equal(t, protocol.MessageTypeResponse, in[1].kind)
	assert.Equal(t, "tools/call", in[1].correlated, "a tools/call answered on an SSE stream must be correlated")
	assert.Equal(t, "s9", in[1].session)
}

func TestHTTPProxy_SSEIsStreamedNotBuffered(t *testing.T) {
	release := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n")
	})
	p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil)

	resp := openStream(t, http.MethodPost, p.URL(), toolCall, nil)
	defer func() { _ = resp.Body.Close() }()
	reader := protocol.NewSSEReader(resp.Body, 0)

	first, err := reader.Next()
	require.NoError(t, err, "the first event must arrive while the upstream still holds the second")
	data, _ := first.Data()
	assert.Contains(t, string(data), "notifications/progress")

	close(release)
	second, err := reader.Next()
	require.NoError(t, err)
	data, _ = second.Data()
	assert.Contains(t, string(data), `"id":7`)
}

func TestHTTPProxy_ResponseOnTheGETStreamIsCorrelated(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if r.Method == http.MethodPost {
			_, _ = io.WriteString(w, "id: 1\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
			return
		}
		assert.Equal(t, "1", r.Header.Get("Last-Event-ID"), "Last-Event-ID must pass for resumption")
		_, _ = io.WriteString(w, "id: 2\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n")
		_, _ = io.WriteString(w, "id: 3\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
	})
	rec := &recorder{}
	pipeline := NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(rec.stage())
	p := startHTTPProxy(t, up.URL, pipeline, zerolog.Nop(), nil)

	_ = call(t, http.MethodPost, p.URL(), toolCall, map[string]string{"Mcp-Session-Id": "s1"}).body
	body := call(t, http.MethodGet, p.URL(), "", map[string]string{"Mcp-Session-Id": "s1", "Last-Event-ID": "1"}).body
	assert.Contains(t, body, `"id":7`)
	assert.Contains(t, body, "list_changed", "a server notification on the GET stream reaches the agent")

	var correlated []string
	for _, s := range rec.incoming() {
		if s.kind == protocol.MessageTypeResponse {
			correlated = append(correlated, s.correlated)
		}
	}
	assert.Equal(t, []string{"tools/call"}, correlated)
}

func TestHTTPProxy_Headers(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", "sess-up")
		w.Header().Set("Connection", "X-Up-Hop")
		w.Header().Set("X-Up-Hop", "1")
		w.Header().Set("Proxy-Authenticate", "Basic")
		w.Header().Set("X-End", "kept")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":7,"result":{}}`)
	})
	p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), []proxyOpt{func(c *HTTPConfig) {
		c.Headers = map[string]string{"Authorization": "Bearer configured", "X-Tenant": "acme"}
	}})

	resp := call(t, http.MethodPost, p.URL(), toolCall, map[string]string{
		"Mcp-Session-Id":       "sess-1",
		"MCP-Protocol-Version": "2025-06-18",
		"Authorization":        "Bearer from-agent",
		"Connection":           "X-Hop",
		"X-Hop":                "1",
		"Proxy-Authorization":  "Basic x",
		"Keep-Alive":           "timeout=5",
		"Origin":               "http://127.0.0.1:9",
		"X-Agent":              "kept",
	})
	_ = resp.body

	require.Len(t, up.recorded(), 1)
	got := up.recorded()[0].header
	assert.Equal(t, "sess-1", got.Get("Mcp-Session-Id"))
	assert.Equal(t, "2025-06-18", got.Get("MCP-Protocol-Version"))
	assert.Equal(t, "Bearer configured", got.Get("Authorization"), "a configured header wins over the agent's")
	assert.Equal(t, "acme", got.Get("X-Tenant"))
	assert.Equal(t, "kept", got.Get("X-Agent"))
	for _, hop := range []string{"X-Hop", "Proxy-Authorization", "Keep-Alive", "Origin"} {
		assert.Empty(t, got.Get(hop), "%s must not be forwarded", hop)
	}

	assert.Equal(t, "sess-up", resp.header.Get("Mcp-Session-Id"))
	assert.Equal(t, "kept", resp.header.Get("X-End"))
	assert.Empty(t, resp.header.Get("X-Up-Hop"))
	assert.Empty(t, resp.header.Get("Proxy-Authenticate"))
}

func TestHTTPProxy_MethodsAndStatuses(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	})
	p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil)

	tests := []struct {
		name   string
		method string
		target string
		body   string
		header map[string]string
		want   int
	}{
		{"a notification is accepted", http.MethodPost, p.URL(), `{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil, http.StatusAccepted},
		{"GET 405 passes through", http.MethodGet, p.URL(), "", nil, http.StatusMethodNotAllowed},
		{"DELETE ends the session upstream", http.MethodDelete, p.URL(), "", map[string]string{"Mcp-Session-Id": "s1"}, http.StatusNoContent},
		{"another path is not the endpoint", http.MethodPost, origin(p) + "/.well-known/oauth-protected-resource", "", nil, http.StatusNotFound},
		{"the bare /mcp path is not the endpoint", http.MethodPost, origin(p) + "/mcp", toolCall, nil, http.StatusNotFound},
		{"a wrong secret is not the endpoint", http.MethodPost, origin(p) + "/mcp/" + strings.Repeat("0", 32), toolCall, nil, http.StatusNotFound},
		{"a cross-site browser fetch is refused", http.MethodPost, p.URL(), toolCall, map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"a same-site browser fetch is refused", http.MethodPost, p.URL(), toolCall, map[string]string{"Sec-Fetch-Site": "same-site"}, http.StatusForbidden},
		{"a navigation is refused", http.MethodGet, p.URL(), "", map[string]string{"Sec-Fetch-Mode": "navigate"}, http.StatusForbidden},
		{"a no-cors fetch is refused", http.MethodGet, p.URL(), "", map[string]string{"Sec-Fetch-Mode": "no-cors"}, http.StatusForbidden},
		{"PUT is not a Streamable HTTP method", http.MethodPut, p.URL(), "", nil, http.StatusMethodNotAllowed},
		{"a foreign Host is refused", http.MethodPost, p.URL(), toolCall, map[string]string{"Host": "evil.example"}, http.StatusForbidden},
		{"a foreign Origin is refused", http.MethodPost, p.URL(), toolCall, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			require.NoError(t, err)
			for k, v := range tt.header {
				if k == "Host" {
					req.Host = v
					continue
				}
				req.Header.Set(k, v)
			}
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, tt.want, resp.StatusCode)
		})
	}

	var deletes []upstreamCall
	for _, c := range up.recorded() {
		if c.method == http.MethodDelete {
			deletes = append(deletes, c)
		}
	}
	require.Len(t, deletes, 1)
	assert.Equal(t, "s1", deletes[0].header.Get("Mcp-Session-Id"))
}

func TestHTTPProxy_RejectsWhatItDoesNotProxy(t *testing.T) {
	up := newUpstream(t, jsonUpstream(t, `{}`))
	p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil, WithMaxPayload(256))

	tests := []struct {
		name     string
		body     string
		status   int
		code     int
		contains string
	}{
		{"a batch", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, http.StatusBadRequest, -32600, "batches"},
		{"not JSON", `{"jsonrpc":`, http.StatusBadRequest, -32700, "not a JSON-RPC message"},
		{"not JSON-RPC", `{"hello":"world"}`, http.StatusBadRequest, -32700, "not a JSON-RPC message"},
		{"over the size cap", `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"x":"` + strings.Repeat("a", 512) + `"}}`, http.StatusRequestEntityTooLarge, -32600, "256 byte limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := call(t, http.MethodPost, p.URL(), tt.body, nil)
			assert.Equal(t, tt.status, resp.status)
			e := decodeRPCError(t, resp.body)
			assert.Equal(t, tt.code, e.Error.Code)
			assert.Contains(t, e.Error.Message, tt.contains)
		})
	}
	assert.Empty(t, up.recorded(), "nothing rejected may reach the server")
}

func TestHTTPProxy_UpstreamSizeCaps(t *testing.T) {
	big := strings.Repeat("x", 2048)
	tests := []struct {
		name    string
		handler http.HandlerFunc
		sse     bool
	}{
		{"a JSON body over the cap", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":7,"result":{"t":"%s"}}`, big)
		}, false},
		{"an SSE event over the cap", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"t\":\"%s\"}}\n\n", big)
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := newUpstream(t, tt.handler)
			p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil, WithMaxPayload(1024))
			resp := call(t, http.MethodPost, p.URL(), toolCall, nil)
			body := resp.body
			if tt.sse {
				ev, err := protocol.NewSSEReader(strings.NewReader(body), 0).Next()
				require.NoError(t, err)
				data, _ := ev.Data()
				body = string(data)
			} else {
				assert.Equal(t, http.StatusBadGateway, resp.status)
			}
			e := decodeRPCError(t, body)
			assert.JSONEq(t, `7`, string(e.ID), "the agent's request id must be answered")
			assert.Contains(t, e.Error.Message, "1024 byte limit")
			assert.Equal(t, 1, p.Stats().Failures)
		})
	}
}

func TestHTTPProxy_UpstreamTimeoutIsNotAGremlin(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		sse     bool
	}{
		{"no response headers", func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}, false},
		{"an SSE stream that goes silent", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, ": open\n\n")
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := newUpstream(t, tt.handler)
			rec := &recorder{}
			pipeline := NewPipeline(zerolog.Nop())
			pipeline.RegisterHandler(rec.stage())
			p := startHTTPProxy(t, up.URL, pipeline, zerolog.Nop(), []proxyOpt{func(c *HTTPConfig) {
				c.RequestTimeout = 200 * time.Millisecond
			}})

			resp := call(t, http.MethodPost, p.URL(), toolCall, nil)
			body := resp.body
			if tt.sse {
				r := protocol.NewSSEReader(strings.NewReader(body), 0)
				var data []byte
				for {
					ev, err := r.Next()
					require.NoError(t, err)
					if d, ok := ev.Data(); ok {
						data = d
						break
					}
				}
				body = string(data)
			} else {
				assert.Equal(t, http.StatusGatewayTimeout, resp.status)
			}
			e := decodeRPCError(t, body)
			assert.JSONEq(t, `7`, string(e.ID))
			assert.Equal(t, UpstreamErrorCode, e.Error.Code, "a real timeout must not look like the timeout gremlin's -32001")
			assert.Contains(t, e.Error.Message, "did not answer within 200ms")
			assert.Equal(t, HTTPStats{Timeouts: 1}, p.Stats())
			assert.Empty(t, rec.incoming(), "a synthesized timeout never goes through the gremlins")
		})
	}
}

func TestHTTPProxy_UnreachableUpstream(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL + "/mcp?token=query-secret"
	dead.Close()

	var logs syncBuffer
	p := startHTTPProxy(t, deadURL, nil, zerolog.New(&logs), nil)
	resp := call(t, http.MethodPost, p.URL(), toolCall, nil)
	assert.Equal(t, http.StatusBadGateway, resp.status)
	e := decodeRPCError(t, resp.body)
	assert.JSONEq(t, `7`, string(e.ID))
	assert.Equal(t, 1, p.Stats().Failures)
	assert.NotContains(t, logs.String(), "query-secret", "the URL's query is never logged")
}

func TestHTTPProxy_Unauthorized(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="https://upstream.example/.well-known/oauth-protected-resource"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
	var logs syncBuffer
	p := startHTTPProxy(t, up.URL, nil, zerolog.New(&logs).Level(zerolog.DebugLevel), []proxyOpt{func(c *HTTPConfig) {
		c.Headers = map[string]string{"Authorization": "Bearer s3cret-token"}
	}})

	resp := call(t, http.MethodPost, p.URL(), toolCall, nil)
	_ = resp.body
	assert.Equal(t, http.StatusUnauthorized, resp.status)
	assert.Empty(t, resp.header.Get("WWW-Authenticate"), "the upstream's OAuth challenge cannot be answered through the proxy")
	require.ErrorIs(t, p.Diagnostic(), ErrUpstreamUnauthorized)
	assert.Equal(t, "Bearer s3cret-token", up.recorded()[0].header.Get("Authorization"))
	assert.NotContains(t, logs.String(), "s3cret-token", "a header value is never logged")
}

func TestHTTPProxy_LegacyTransportIsDiagnosed(t *testing.T) {
	legacy := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: endpoint\ndata: /messages?sessionId=1\n\n")
	}

	t.Run("initialize POST, then the spec's probe", func(t *testing.T) {
		up := newUpstream(t, legacy)
		p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil)
		resp := call(t, http.MethodPost, p.URL(),
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
		assert.Equal(t, http.StatusBadGateway, resp.status)
		e := decodeRPCError(t, resp.body)
		assert.JSONEq(t, `1`, string(e.ID))
		assert.Contains(t, e.Error.Message, "legacy HTTP+SSE")
		assert.ErrorIs(t, p.Diagnostic(), ErrLegacyTransport)
	})

	t.Run("a GET stream that starts with an endpoint event", func(t *testing.T) {
		up := newUpstream(t, legacy)
		p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil)
		body := call(t, http.MethodGet, p.URL(), "", nil).body
		assert.NotContains(t, body, "endpoint")
		assert.ErrorIs(t, p.Diagnostic(), ErrLegacyTransport)
	})

	t.Run("a 404 from a Streamable HTTP server is not mistaken for it", func(t *testing.T) {
		up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			http.Error(w, "session expired", http.StatusNotFound)
		})
		p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil)
		resp := call(t, http.MethodPost, p.URL(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
		_ = resp.body
		assert.Equal(t, http.StatusNotFound, resp.status)
		assert.NoError(t, p.Diagnostic())
	})
}

func TestHTTPProxy_RedirectIsNotFollowed(t *testing.T) {
	target := newUpstream(t, jsonUpstream(t, `{}`))
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	p := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), []proxyOpt{func(c *HTTPConfig) {
		c.Headers = map[string]string{"Authorization": "Bearer s3cret"}
	}})
	resp := call(t, http.MethodPost, p.URL(), toolCall, nil)
	assert.Equal(t, http.StatusBadGateway, resp.status)
	e := decodeRPCError(t, resp.body)
	assert.JSONEq(t, `7`, string(e.ID))
	assert.ErrorIs(t, p.Diagnostic(), ErrUpstreamRedirect)
	assert.Empty(t, target.recorded(), "the credentials must never reach the redirect target")
}

func TestHTTPProxy_PipelineDecisions(t *testing.T) {
	isToolCall := func(msg *protocol.Message, mctx *MessageContext) bool {
		return msg.GetMethod() == "tools/call" ||
			(mctx.CorrelatedRequest != nil && mctx.CorrelatedRequest.GetMethod() == "tools/call")
	}
	tests := []struct {
		name         string
		upstream     func(*testing.T, string) http.HandlerFunc
		stage        func(context.Context, *protocol.Message, *MessageContext) (*Decision, error)
		wantUpstream bool
		check        func(t *testing.T, agentBody string, upstreamBody string)
	}{
		{
			name:     "a blocked request is answered with an error carrying its id",
			upstream: jsonUpstream,
			stage: func(_ context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
				if mctx.Direction == models.DirectionOutgoing && isToolCall(msg, mctx) {
					return &Decision{Action: DecisionBlock, Reason: "nope"}, nil
				}
				return nil, nil
			},
			check: func(t *testing.T, agentBody, _ string) {
				e := decodeRPCError(t, agentBody)
				assert.JSONEq(t, `7`, string(e.ID))
				assert.Contains(t, e.Error.Message, "Blocked by Gremlyn: nope")
			},
		},
		{
			name:         "a blocked response on an SSE stream becomes an error event",
			upstream:     sseUpstream,
			wantUpstream: true,
			stage: func(_ context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
				if mctx.Direction == models.DirectionIncoming && msg.Type == protocol.MessageTypeResponse {
					return &Decision{Action: DecisionBlock, Reason: "held"}, nil
				}
				return nil, nil
			},
			check: func(t *testing.T, agentBody, _ string) {
				assert.Contains(t, agentBody, "Blocked by Gremlyn: held")
				assert.Contains(t, agentBody, "id: e2\n", "the SSE event id survives a rewrite")
				assert.Contains(t, agentBody, "notifications/progress")
			},
		},
		{
			name:         "a modified request is what the server receives",
			upstream:     jsonUpstream,
			wantUpstream: true,
			stage: func(_ context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
				if mctx.Direction != models.DirectionOutgoing || msg.Request == nil {
					return nil, nil
				}
				mod := *msg
				req := *msg.Request
				req.Params = json.RawMessage(`{"name":"renamed"}`)
				mod.Request = &req
				return &Decision{Action: DecisionModify, ModifiedMessage: &mod}, nil
			},
			check: func(t *testing.T, _, upstreamBody string) {
				assert.Contains(t, upstreamBody, `"renamed"`)
				assert.Contains(t, upstreamBody, `"id":7`, "the envelope is kept")
			},
		},
		{
			name:         "a modified response is what the agent receives, with its id",
			upstream:     jsonUpstream,
			wantUpstream: true,
			stage: func(_ context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
				if mctx.Direction != models.DirectionIncoming || msg.Response == nil {
					return nil, nil
				}
				mod := *msg
				resp := *msg.Response
				resp.Result = json.RawMessage(`{"mutated":true}`)
				mod.Response = &resp
				return &Decision{Action: DecisionModify, ModifiedMessage: &mod}, nil
			},
			check: func(t *testing.T, agentBody, _ string) {
				assert.JSONEq(t, `{"jsonrpc":"2.0","id":7,"result":{"mutated":true}}`, agentBody)
			},
		},
		{
			name:         "a panicking stage is contained",
			upstream:     jsonUpstream,
			wantUpstream: true,
			stage: func(context.Context, *protocol.Message, *MessageContext) (*Decision, error) {
				panic("boom")
			},
			check: func(t *testing.T, agentBody, _ string) {
				assert.Contains(t, agentBody, `"ok"`)
			},
		},
		{
			name:         "an erroring stage is skipped",
			upstream:     jsonUpstream,
			wantUpstream: true,
			stage: func(context.Context, *protocol.Message, *MessageContext) (*Decision, error) {
				return nil, errors.New("stage failed")
			},
			check: func(t *testing.T, agentBody, _ string) {
				assert.Contains(t, agentBody, `"ok"`)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := newUpstream(t, tt.upstream(t, `{"ok":true}`))
			pipeline := NewPipeline(zerolog.Nop())
			pipeline.RegisterHandler(&funcStage{fn: tt.stage})
			p := startHTTPProxy(t, up.URL, pipeline, zerolog.Nop(), nil)

			agentBody := call(t, http.MethodPost, p.URL(), toolCall, nil).body
			calls := up.recorded()
			if !tt.wantUpstream {
				assert.Empty(t, calls)
				tt.check(t, agentBody, "")
				return
			}
			require.Len(t, calls, 1)
			tt.check(t, agentBody, calls[0].body)
		})
	}
}

func TestHTTPProxy_SlowExchangeDoesNotHoldOthers(t *testing.T) {
	up := newUpstream(t, jsonUpstream(t, `{}`))
	hold := make(chan struct{})
	held := make(chan struct{})
	pipeline := NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(&funcStage{fn: func(ctx context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
		if mctx.Direction == models.DirectionIncoming && mctx.CorrelatedRequest != nil &&
			mctx.CorrelatedRequest.GetMethod() == "tools/call" {
			close(held)
			select {
			case <-hold:
			case <-ctx.Done():
			}
		}
		return nil, nil
	}})
	p := startHTTPProxy(t, up.URL, pipeline, zerolog.Nop(), nil)

	slow := make(chan string, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, p.URL(), strings.NewReader(toolCall))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slow <- err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		slow <- string(b)
	}()
	<-held

	fast := call(t, http.MethodPost, p.URL(), `{"jsonrpc":"2.0","id":8,"method":"ping"}`, nil).body
	assert.Contains(t, fast, `"id": 8`, "another exchange completes while the targeted one is held")

	close(hold)
	assert.Contains(t, <-slow, `"id": 7`)
}

func TestHTTPProxy_ShutdownDrainsInFlightExchanges(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		jsonUpstream(t, `{"drained":true}`)(w, r)
	})
	hold := make(chan struct{})
	held := make(chan struct{})
	pipeline := NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(&funcStage{fn: func(_ context.Context, msg *protocol.Message, mctx *MessageContext) (*Decision, error) {
		if mctx.Direction == models.DirectionIncoming && msg.Type == protocol.MessageTypeResponse {
			close(held)
			<-hold
		}
		return nil, nil
	}})
	cfg := HTTPConfig{ServerName: "fake", URL: up.URL}
	p, err := NewHTTPProxy(cfg, WithLogger(zerolog.Nop()), WithPipeline(pipeline))
	require.NoError(t, err)
	require.NoError(t, p.Start(context.Background()))
	t.Cleanup(pipeline.Close)

	stream := openStream(t, http.MethodGet, p.URL(), "", nil)
	defer func() { _ = stream.Body.Close() }()
	require.Equal(t, http.StatusOK, stream.StatusCode)

	answer := make(chan string, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, p.URL(), strings.NewReader(toolCall))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			answer <- err.Error()
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		answer <- string(b)
	}()
	<-held

	stopped := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		stopped <- p.Shutdown(ctx)
	}()
	close(hold)

	assert.Contains(t, <-answer, `"drained":true`, "a response in flight at shutdown still reaches the agent")
	require.NoError(t, <-stopped)
	_, err = io.ReadAll(stream.Body)
	assert.NoError(t, err, "the GET stream ends cleanly at shutdown")
}

func TestValidateUpstreamURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr string
	}{
		{"https://mcp.example.com/mcp", ""},
		{"http://127.0.0.1:8080/mcp", ""},
		{"http://localhost:8080/mcp", ""},
		{"http://[::1]:8080/mcp", ""},
		{"http://mcp.example.com/mcp", "plain http"},
		{"http://10.0.0.2/mcp", "plain http"},
		{"ftp://mcp.example.com", "not supported"},
		{"https://user:pass@mcp.example.com", "credentials"},
		{"https:///mcp", "host"},
		{"https://mcp.example.com/mcp#frag", "fragment"},
		{"://nope", "not a valid URL"},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			_, err := ValidateUpstreamURL(tt.url)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.NotContains(t, err.Error(), "pass", "credentials are never echoed")
		})
	}
}

func TestValidateListenAddr(t *testing.T) {
	tests := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:0", true},
		{"[::1]:4000", true},
		{"localhost:4000", true},
		{"0.0.0.0:4000", false},
		{":4000", false},
		{"192.168.1.2:4000", false},
		{"nonsense", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			assert.Equal(t, tt.ok, ValidateListenAddr(tt.addr) == nil)
		})
	}
}

func TestNewHTTPProxy_RefusesReservedHeaders(t *testing.T) {
	for _, name := range []string{"mcp-session-id", "Host", "Connection", "Content-Length", "MCP-Protocol-Version"} {
		t.Run(name, func(t *testing.T) {
			_, err := NewHTTPProxy(HTTPConfig{URL: "https://mcp.example.com", Headers: map[string]string{name: "x"}})
			require.Error(t, err)
		})
	}
	_, err := NewHTTPProxy(HTTPConfig{URL: "https://mcp.example.com", Headers: map[string]string{"Authorization": "x"}})
	assert.NoError(t, err)
	_, err = NewHTTPProxy(HTTPConfig{URL: "https://mcp.example.com", ListenAddr: "0.0.0.0:0"})
	assert.Error(t, err)
}

func TestRedactURL(t *testing.T) {
	_, err := ValidateUpstreamURL("https://mcp.example.com/mcp?key=secret")
	require.NoError(t, err)
	u, _ := ValidateUpstreamURL("https://mcp.example.com/mcp?key=secret")
	assert.Equal(t, "https://mcp.example.com/mcp", RedactURL(u))
	assert.Empty(t, RedactURL(nil))
}

func BenchmarkHTTPProxy_PostJSON(b *testing.B) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"ok"}]}}`)
	}))
	defer up.Close()
	pipeline := NewPipeline(zerolog.Nop())
	defer pipeline.Close()
	p, err := NewHTTPProxy(HTTPConfig{URL: up.URL}, WithPipeline(pipeline))
	require.NoError(b, err)
	require.NoError(b, p.Start(context.Background()))
	defer func() { _ = p.Shutdown(context.Background()) }()

	b.ReportAllocs()
	for b.Loop() {
		resp, err := http.Post(p.URL(), "application/json", strings.NewReader(toolCall))
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func BenchmarkHTTPUpstreamDirect(b *testing.B) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"ok"}]}}`)
	}))
	defer up.Close()
	b.ReportAllocs()
	for b.Loop() {
		resp, err := http.Post(up.URL, "application/json", strings.NewReader(toolCall))
		if err != nil {
			b.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

func TestHTTPProxy_UpstreamTextNeverReachesTheLogs(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"attacker_chosen_key\":\"payload-text\"}\n\n")
		_, _ = io.WriteString(w, "data: not json at all payload-text\n\n")
		_, _ = io.WriteString(w, "data: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{}}\n\n")
	})
	var logs syncBuffer
	p := startHTTPProxy(t, up.URL, nil, zerolog.New(&logs).Level(zerolog.DebugLevel), nil)
	body := call(t, http.MethodPost, p.URL(), toolCall, nil).body
	assert.Contains(t, body, "attacker_chosen_key", "a message the proxy cannot parse is forwarded untouched")
	assert.Contains(t, logs.String(), "not a JSON-RPC message")
	assert.NotContains(t, logs.String(), "attacker_chosen_key")
	assert.NotContains(t, logs.String(), "payload-text")
}

func origin(p *HTTPProxy) string {
	u, err := url.Parse(p.URL())
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func TestHTTPProxy_SecretPath(t *testing.T) {
	up := newUpstream(t, jsonUpstream(t, `{}`))
	var logs syncBuffer
	first := startHTTPProxy(t, up.URL, nil, zerolog.New(&logs).Level(zerolog.DebugLevel), nil)
	second := startHTTPProxy(t, up.URL, nil, zerolog.Nop(), nil)

	assert.Regexp(t, `^http://127\.0\.0\.1:[0-9]+/mcp/[0-9a-f]{32}$`, first.URL())
	assert.NotEqual(t, strings.TrimPrefix(first.URL(), origin(first)), strings.TrimPrefix(second.URL(), origin(second)),
		"each proxy draws its own secret path")

	for _, target := range []string{origin(first) + "/mcp", origin(first) + "/mcp/", first.URL() + "x", origin(first) + strings.TrimPrefix(second.URL(), origin(second))} {
		assert.Equal(t, http.StatusNotFound, call(t, http.MethodPost, target, toolCall, nil).status, target)
	}
	assert.Empty(t, up.recorded(), "a request without the secret path never reaches the upstream")

	assert.Equal(t, http.StatusOK, call(t, http.MethodPost, first.URL(), toolCall, nil).status)
	assert.NotContains(t, logs.String(), strings.TrimPrefix(first.URL(), origin(first)+"/mcp/"), "the secret path is never logged")
}

func TestLoopbackOnlyControl(t *testing.T) {
	tests := []struct {
		address string
		ok      bool
	}{
		{"127.0.0.1:80", true},
		{"127.0.0.2:80", true},
		{"[::1]:80", true},
		{"10.0.0.1:80", false},
		{"93.184.216.34:443", false},
		{"[2001:db8::1]:80", false},
		{"no-port", false},
	}
	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			assert.Equal(t, tt.ok, loopbackOnlyControl("tcp", tt.address, nil) == nil)
		})
	}
}

func TestRequireLoopbackAddr(t *testing.T) {
	assert.NoError(t, requireLoopbackAddr(&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}))
	assert.NoError(t, requireLoopbackAddr(&net.TCPAddr{IP: net.IPv6loopback, Port: 1}))
	assert.Error(t, requireLoopbackAddr(&net.TCPAddr{IP: net.IPv4(192, 168, 1, 2), Port: 1}),
		"a localhost that resolves off the machine must not be served")
	assert.Error(t, requireLoopbackAddr(&net.UnixAddr{Name: "/tmp/x", Net: "unix"}))
}

func TestHTTPProxy_PlainHTTPToLocalhostByName(t *testing.T) {
	up := newUpstream(t, jsonUpstream(t, `{"ok":true}`))
	u, err := url.Parse(up.URL)
	require.NoError(t, err)
	p := startHTTPProxy(t, "http://localhost:"+u.Port(), nil, zerolog.Nop(), nil)
	assert.Contains(t, call(t, http.MethodPost, p.URL(), toolCall, nil).body, `"ok"`)
}
