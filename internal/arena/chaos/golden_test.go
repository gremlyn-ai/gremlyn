package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite the golden file")

type step struct {
	outgoing bool
	raw      string
}

type goldenLine struct {
	Run       string               `json:"run"`
	I         int                  `json:"i"`
	Action    proxy.DecisionAction `json:"action"`
	Reason    string               `json:"reason,omitempty"`
	Out       string               `json:"out"`
	Gremlin   string               `json:"gremlin,omitempty"`
	Tool      string               `json:"tool,omitempty"`
	Method    string               `json:"method,omitempty"`
	RequestID string               `json:"request_id,omitempty"`
	Succeeded bool                 `json:"succeeded,omitempty"`
}

func call(id int, tool string) step {
	return step{true, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, id, tool)}
}

func reply(id int, result string) step {
	return step{false, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, id, result)}
}

const okResult = `{"content":[{"type":"text","text":"no notes"}]}`

func goldenTranscript() []step {
	steps := []step{
		{true, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"golden","version":"0"}}}`},
		{false, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"notes","version":"0"}}}`},
		{true, `{"jsonrpc":"2.0","method":"notifications/initialized"}`},
		{true, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`},
		{false, `{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"list_notes","inputSchema":{"type":"object"}},{"name":"create_note","inputSchema":{"type":"object"}}]}}`},
	}
	for r := 1; r <= 6; r++ {
		id := r * 10
		steps = append(steps,
			call(id, "list_notes"), reply(id, okResult),
			call(id+1, "create_note"), reply(id+1, `{"content":[{"type":"text","text":"a note needs a title"}],"isError":true}`),
			call(id+2, "create_note"), step{false, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":-32602,"message":"rejected"}}`, id+2)},
			step{false, `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":1,"progress":1}}`},
			step{true, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":"oops"}`, id+3)},
		)
	}
	return steps
}

func goldenRuns() [][]string {
	runs := make([][]string, 0, len(KnownGremlins())+1)
	for _, name := range KnownGremlins() {
		runs = append(runs, []string{name})
	}
	return append(runs, KnownGremlins())
}

func buildGolden(t *testing.T, names []string, intensity Intensity) []gremlins.Gremlin {
	t.Helper()
	gs, err := BuildGremlinsWithParams(names, 42, intensity, GremlinParams{
		MinDelayMs: 1, MaxDelayMs: 2, MinTimeoutMs: 1, MaxTimeoutMs: 2, PayloadBytes: 256,
	})
	require.NoError(t, err)
	return gs
}

func runTranscript(t *testing.T, run string, gs []gremlins.Gremlin, steps []step, opts ...HandlerOption) []goldenLine {
	t.Helper()
	log := NewInjectionLog()
	pipeline := proxy.NewPipeline(zerolog.Nop())
	t.Cleanup(pipeline.Close)
	pipeline.RegisterHandler(NewGremlinHandler(gs, log, zerolog.Nop(), opts...))
	lines := make([]goldenLine, 0, len(steps))
	for i, s := range steps {
		msg, err := protocol.ParseMessage([]byte(s.raw))
		require.NoError(t, err)
		dir := models.DirectionIncoming
		if s.outgoing {
			dir = models.DirectionOutgoing
		}
		before := log.Len()
		dec, fwd, err := pipeline.Process(context.Background(), msg, &proxy.MessageContext{ServerName: "golden", Direction: dir})
		require.NoError(t, err)
		out, err := protocol.EncodeMessage(fwd)
		require.NoError(t, err)
		line := goldenLine{Run: run, I: i, Action: dec.Action, Reason: dec.Reason, Out: string(out)}
		if injs := log.Injections(); len(injs) > before {
			inj := injs[len(injs)-1]
			line.Gremlin, line.Tool, line.Method, line.RequestID, line.Succeeded = inj.GremlinName, inj.Tool, inj.Method, inj.RequestID, inj.Succeeded
		}
		lines = append(lines, line)
	}
	return lines
}

func TestGremlinHandler_UntargetedOutputIsPinned(t *testing.T) {
	path := filepath.Join("testdata", "untargeted.golden.jsonl")
	render := func(opts ...HandlerOption) []byte {
		var got bytes.Buffer
		enc := json.NewEncoder(&got)
		for _, run := range goldenRuns() {
			for _, line := range runTranscript(t, strings.Join(run, "+"), buildGolden(t, run, IntensityMedium), goldenTranscript(), opts...) {
				require.NoError(t, enc.Encode(line))
			}
		}
		return got.Bytes()
	}
	got := render()
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, got, 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
	assert.Equal(t, string(want), string(render(WithTargets(nil))))
}
