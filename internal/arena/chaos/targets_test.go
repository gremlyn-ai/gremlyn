package chaos

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var createNote = WithTargets([]string{"create_note"})

func targetedTranscript(between, rounds int) []step {
	var steps []step
	for r := 1; r <= rounds; r++ {
		for j := range between {
			id := 1000 + r*10 + j
			steps = append(steps, call(id, "list_notes"), reply(id, okResult))
		}
		steps = append(steps, call(r, "create_note"), reply(r, okResult))
	}
	return steps
}

func injected(lines []goldenLine) int {
	n := 0
	for _, l := range lines {
		if l.Gremlin != "" {
			n++
		}
	}
	return n
}

func TestGremlinHandler_TargetsHitOnlyTheTarget(t *testing.T) {
	for _, run := range goldenRuns() {
		t.Run(strings.Join(run, "+"), func(t *testing.T) {
			lines := runTranscript(t, "", buildGolden(t, run, IntensityCertain), targetedTranscript(2, 1), createNote)
			require.Len(t, lines, 6)
			for _, l := range lines[:4] {
				assert.Equal(t, proxy.DecisionAllow, l.Action, "list_notes is not a target")
				assert.Empty(t, l.Gremlin)
			}
			assert.Positive(t, injected(lines[4:]), "the target exchange is hit")
			for _, l := range lines[4:] {
				if l.Gremlin != "" {
					assert.Equal(t, "create_note", l.Tool)
				}
			}
		})
	}
}

func TestGremlinHandler_NonTargetPassesThroughByteExact(t *testing.T) {
	req := func(params string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":%s}`, params)
	}
	named := func(name string) string { return req(fmt.Sprintf(`{"name":%q}`, name)) }
	result := `{"jsonrpc":"2.0","id":7,"result":{"content":[]}}`
	tests := []struct {
		name, raw, correlated string
		hit                   bool
	}{
		{"the target request", named("create_note"), "", true},
		{"the target response", result, named("create_note"), true},
		{"a non-target request", named("list_notes"), "", false},
		{"a non-target success", result, named("list_notes"), false},
		{"a non-target JSON-RPC error", `{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"rejected"}}`, named("list_notes"), false},
		{"a non-target isError result", `{"jsonrpc":"2.0","id":7,"result":{"content":[],"isError":true}}`, named("list_notes"), false},
		{"a notification", `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":1}}`, "", false},
		{"undecodable params", req(`"oops"`), "", false},
		{"a missing name", req(`{}`), "", false},
		{"a non-string name", req(`{"name":5}`), "", false},
		{"another case", named("Create_Note"), "", false},
		{"a leading space", named(" create_note"), "", false},
		{"a trailing space", named("create_note "), "", false},
		{"a target response past the correlator TTL", result, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			counting := &fakeGremlin{name: "counting", fn: func(_ context.Context, m *protocol.Message) (*protocol.Message, bool, error) {
				calls++
				return cloneResponse(m, `{}`), true, nil
			}}
			msg, err := protocol.ParseMessage([]byte(tt.raw))
			require.NoError(t, err)
			mctx := &proxy.MessageContext{}
			if tt.correlated != "" {
				mctx.CorrelatedRequest, err = protocol.ParseMessage([]byte(tt.correlated))
				require.NoError(t, err)
			}
			before, err := protocol.EncodeMessage(msg)
			require.NoError(t, err)

			h := NewGremlinHandler([]gremlins.Gremlin{counting}, NewInjectionLog(), zerolog.Nop(), createNote)
			dec, err := h.HandleMessage(context.Background(), msg, mctx)
			require.NoError(t, err)

			if tt.hit {
				assert.Equal(t, proxy.DecisionModify, dec.Action)
				assert.Equal(t, 1, calls)
				return
			}
			after, err := protocol.EncodeMessage(msg)
			require.NoError(t, err)
			assert.Equal(t, proxy.DecisionSkip, dec.Action)
			assert.Nil(t, dec.ModifiedMessage)
			assert.Equal(t, string(before), string(after))
			assert.Zero(t, calls, "a non-target must not reach a gremlin")
		})
	}
}

func TestGremlinHandler_TargetStatusFollowsTheOriginalRequest(t *testing.T) {
	gs := buildGolden(t, []string{"hallucination", "corruption"}, IntensityCertain)
	lines := runTranscript(t, "", gs, targetedTranscript(0, 1), createNote)
	require.Len(t, lines, 2)
	assert.Equal(t, "hallucination", lines[0].Gremlin)
	assert.Contains(t, lines[0].Out, "nonexistent_tool")
	assert.Equal(t, "corruption", lines[1].Gremlin, "the response answers create_note, whatever the gremlin renamed it to")
	assert.Equal(t, "create_note", lines[1].Tool)
}

func TestGremlinHandler_NonTargetCallsDrawNoRandomness(t *testing.T) {
	const rounds = 20
	targetLines := func(lines []goldenLine, between int) []goldenLine {
		var out []goldenLine
		for _, l := range lines {
			if l.I%(2*between+2) < 2*between {
				assert.Empty(t, l.Gremlin, "a non-target must not be injected")
				continue
			}
			l.I = 0
			out = append(out, l)
		}
		return out
	}
	for _, name := range KnownGremlins() {
		for _, intensity := range []Intensity{IntensityLow, IntensityMedium} {
			t.Run(name+"/"+string(intensity), func(t *testing.T) {
				gs := func() []gremlins.Gremlin { return buildGolden(t, []string{name}, intensity) }
				want := targetLines(runTranscript(t, "", gs(), targetedTranscript(0, rounds), createNote), 0)
				for _, between := range []int{1, 5} {
					got := targetLines(runTranscript(t, "", gs(), targetedTranscript(between, rounds), createNote), between)
					assert.Equal(t, want, got, "%d non-target calls before and between the targets", between)
				}
			})
		}
	}
}

func TestGremlinHandler_TargetedRunIsDeterministic(t *testing.T) {
	run := func() []goldenLine {
		return runTranscript(t, "", buildGolden(t, KnownGremlins(), IntensityMedium), targetedTranscript(1, 20), createNote)
	}
	first := run()
	assert.Equal(t, first, run())
	assert.Positive(t, injected(first))
}

func TestValidateTargets(t *testing.T) {
	build := func(name string, params GremlinParams) []gremlins.Gremlin {
		gs, err := BuildGremlinsWithParams([]string{name}, 1, IntensityMedium, params)
		require.NoError(t, err)
		return gs
	}
	plain := build("timeout", GremlinParams{})
	hallucination := build("hallucination", GremlinParams{})
	custom := build("hallucination", GremlinParams{FakeTool: "ghost_tool"})
	tests := []struct {
		name    string
		targets []string
		gs      []gremlins.Gremlin
		wantErr string
	}{
		{"an empty list", []string{}, plain, "the list is empty"},
		{"an empty name", []string{"create_note", ""}, plain, "entry 2 is empty"},
		{"a whitespace-only name", []string{"  "}, plain, "entry 1 is empty"},
		{"a padded name", []string{"create_note "}, plain, `"create_note " has leading or trailing whitespace`},
		{"a duplicate", []string{"create_note", "list_notes", "create_note"}, plain, `"create_note" is listed twice`},
		{"names differing only by case", []string{"create_note", "Create_Note"}, plain, ""},
		{"the default fake tool", []string{"nonexistent_tool"}, hallucination, `"nonexistent_tool" is the tool the hallucination gremlin invents`},
		{"a custom fake tool", []string{"ghost_tool"}, custom, `"ghost_tool" is the tool the hallucination gremlin invents`},
		{"the fake tool name without hallucination", []string{"nonexistent_tool"}, plain, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTargets(tt.targets, tt.gs)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
