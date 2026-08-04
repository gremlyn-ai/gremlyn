package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// These tests exercise the actual request/response path through the wrap proxy.
//
// They exist because every earlier wrap test only covered construction, which let
// a teardown bug ship: the client reaching EOF tore the proxy down before the
// child's responses were drained, so `gremlyn wrap` forwarded a request and never
// returned its answer.

// syncBuffer is an io.Writer safe for concurrent use, since the proxy writes to
// the client stream from its own goroutine while the test reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// echoServerCmd returns a Config for a fake stdio MCP server: a shell loop that
// answers every line it receives with a JSON-RPC result carrying the same id.
func echoServerCmd() Config {
	const script = `while IFS= read -r line; do
	  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
	  [ -n "$id" ] && printf '{"jsonrpc":"2.0","id":%s,"result":{"ok":true}}\n' "$id"
	done`
	return Config{
		ServerName: "fake-mcp",
		Mode:       models.ServerModeWrap,
		Command:    "sh",
		Args:       []string{"-c", script},
	}
}

func requestLine(t *testing.T, id int, method string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	})
	require.NoError(t, err)
	return string(b) + "\n"
}

// TestWrapProxy_ForwardsResponseAfterClientEOF is the regression test for the
// teardown bug. Piping input closes the client stream immediately, which is both
// how a script drives the proxy and how a real client disconnecting behaves — the
// in-flight response must still reach the client.
func TestWrapProxy_ForwardsResponseAfterClientEOF(t *testing.T) {
	in := strings.NewReader(requestLine(t, 1, "tools/list"))
	out := &syncBuffer{}

	p := NewWrapProxy(echoServerCmd(),
		WithLogger(zerolog.Nop()),
		WithClientIO(in, out),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, p.Start(ctx))

	got := out.String()
	require.NotEmpty(t, got, "client received nothing: the response was dropped during teardown")
	assert.Contains(t, got, `"id":1`)
	assert.Contains(t, got, `"ok":true`)
}

// TestWrapProxy_ForwardsMultipleResponses checks that a burst of requests all get
// answered, not just the first.
func TestWrapProxy_ForwardsMultipleResponses(t *testing.T) {
	in := strings.NewReader(
		requestLine(t, 1, "initialize") +
			requestLine(t, 2, "tools/list") +
			requestLine(t, 3, "tools/call"),
	)
	out := &syncBuffer{}

	p := NewWrapProxy(echoServerCmd(),
		WithLogger(zerolog.Nop()),
		WithClientIO(in, out),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, p.Start(ctx))

	got := out.String()
	for _, id := range []string{`"id":1`, `"id":2`, `"id":3`} {
		assert.Contains(t, got, id, "missing response for %s", id)
	}
}

// TestWrapProxy_PipelineSeesBothDirections proves the pipeline is wired on the
// real data path, in both directions — the seam Shield and Arena plug into.
func TestWrapProxy_PipelineSeesBothDirections(t *testing.T) {
	var mu sync.Mutex
	seen := map[models.Direction]int{}

	pipeline := NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(&countingHandler{
		onMessage: func(mctx *MessageContext) {
			mu.Lock()
			defer mu.Unlock()
			seen[mctx.Direction]++
		},
	})

	in := strings.NewReader(requestLine(t, 7, "tools/list"))
	out := &syncBuffer{}

	p := NewWrapProxy(echoServerCmd(),
		WithLogger(zerolog.Nop()),
		WithPipeline(pipeline),
		WithClientIO(in, out),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, p.Start(ctx))

	mu.Lock()
	defer mu.Unlock()
	assert.Positive(t, seen[models.DirectionOutgoing], "pipeline never saw the request")
	assert.Positive(t, seen[models.DirectionIncoming], "pipeline never saw the response")
}

// TestWrapProxy_BlockedRequestGetsJSONRPCError checks that a blocking handler
// produces an error the agent can actually see. A silent drop would leave the
// agent waiting forever, which is worse than a refusal.
func TestWrapProxy_BlockedRequestGetsJSONRPCError(t *testing.T) {
	pipeline := NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(&countingHandler{
		decide: func(mctx *MessageContext) *Decision {
			if mctx.Direction == models.DirectionOutgoing {
				return &Decision{Action: DecisionBlock, Reason: "test policy"}
			}
			return &Decision{Action: DecisionSkip}
		},
	})

	in := strings.NewReader(requestLine(t, 9, "tools/call"))
	out := &syncBuffer{}

	p := NewWrapProxy(echoServerCmd(),
		WithLogger(zerolog.Nop()),
		WithPipeline(pipeline),
		WithClientIO(in, out),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, p.Start(ctx))

	got := out.String()
	require.NotEmpty(t, got, "a blocked request must still produce a visible error")
	assert.Contains(t, got, `"id":9`)
	assert.Contains(t, got, "error")
	assert.Contains(t, got, "test policy")
}

// TestWrapProxy_StopIsIdempotent guards the shutdown path against a double Stop,
// which Start itself can trigger alongside an explicit caller Stop.
func TestWrapProxy_StopIsIdempotent(t *testing.T) {
	in := strings.NewReader(requestLine(t, 1, "tools/list"))
	p := NewWrapProxy(echoServerCmd(),
		WithLogger(zerolog.Nop()),
		WithClientIO(in, io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, p.Start(ctx))
	require.NoError(t, p.Stop())
	require.NoError(t, p.Stop())
}

// countingHandler is a test Handler that observes every message and optionally
// decides on it.
type countingHandler struct {
	onMessage func(mctx *MessageContext)
	decide    func(mctx *MessageContext) *Decision
}

func (h *countingHandler) Name() string                { return "test-counter" }
func (h *countingHandler) Priority() int               { return 100 }
func (h *countingHandler) Direction() models.Direction { return models.DirectionBoth }

func (h *countingHandler) HandleMessage(_ context.Context, _ *protocol.Message, mctx *MessageContext) (*Decision, error) {
	if h.onMessage != nil {
		h.onMessage(mctx)
	}
	if h.decide != nil {
		return h.decide(mctx), nil
	}
	return &Decision{Action: DecisionSkip}, nil
}
