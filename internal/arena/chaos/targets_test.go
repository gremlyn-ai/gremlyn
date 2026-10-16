package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gremlyn-ai/gremlyn/internal/arena/gremlins"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
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
		{"a notification, stopped by the tool-call guard", `{"jsonrpc":"2.0","method":"notifications/progress","params":{"progressToken":1}}`, "", false},
		{"undecodable params", req(`"oops"`), "", false},
		{"a missing name", req(`{}`), "", false},
		{"a non-string name", req(`{"name":5}`), "", false},
		{"another case", named("Create_Note"), "", false},
		{"a leading space", named(" create_note"), "", false},
		{"a trailing space", named("create_note "), "", false},
		{"a response with no correlated request, stopped by the tool-call guard", result, "", false},
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

func listing(id int, cursor string, names ...string) []step {
	tools := make([]string, len(names))
	for i, n := range names {
		tools[i] = fmt.Sprintf(`{"name":%q}`, n)
	}
	next := ""
	if cursor != "" {
		next = fmt.Sprintf(`,"nextCursor":%q`, cursor)
	}
	return []step{
		{true, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/list","params":{}}`, id)},
		{false, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"tools":[%s]%s}}`, id, strings.Join(tools, ","), next)},
	}
}

func feed(t *testing.T, h *GremlinHandler, steps []step) {
	t.Helper()
	pipeline := proxy.NewPipeline(zerolog.Nop())
	t.Cleanup(pipeline.Close)
	pipeline.RegisterHandler(h)
	for _, s := range steps {
		msg, err := protocol.ParseMessage([]byte(s.raw))
		require.NoError(t, err)
		dir := models.DirectionIncoming
		if s.outgoing {
			dir = models.DirectionOutgoing
		}
		_, _, err = pipeline.Process(context.Background(), msg, &proxy.MessageContext{Direction: dir})
		require.NoError(t, err)
	}
}

func TestGremlinHandler_TargetStats(t *testing.T) {
	onResponses := &fakeGremlin{name: "on-responses", fn: func(_ context.Context, m *protocol.Message) (*protocol.Message, bool, error) {
		if m.Response == nil {
			return m, false, nil
		}
		return cloneResponse(m, `{}`), true, nil
	}}
	never := &fakeGremlin{name: "never", fn: func(_ context.Context, m *protocol.Message) (*protocol.Message, bool, error) {
		return m, false, nil
	}}
	concat := slices.Concat[[]step]
	createCall := []step{call(9, "create_note"), reply(9, okResult)}
	tests := []struct {
		name    string
		g       gremlins.Gremlin
		targets []string
		steps   []step
		want    []TargetStat
	}{
		{"never called, absent from a complete listing", onResponses, []string{"create_note"},
			concat(listing(1, "", "list_notes"), []step{call(2, "list_notes"), reply(2, okResult)}),
			[]TargetStat{{Name: "create_note", Listing: ListingNotListed}}},
		{"called and injected, listed without annotations", onResponses, []string{"create_note"},
			concat(listing(1, "", "create_note"), createCall),
			[]TargetStat{{Name: "create_note", Called: 1, Injected: 1, Listing: ListingListed}}},
		{"called, never injected", never, []string{"create_note"},
			concat(listing(1, "", "create_note"), createCall, createCall),
			[]TargetStat{{Name: "create_note", Called: 2, Listing: ListingListed}}},
		{"no listing seen", onResponses, []string{"create_note"}, createCall,
			[]TargetStat{{Name: "create_note", Called: 1, Injected: 1, Listing: ListingUnverified}}},
		{"a page with a cursor is not a complete listing", onResponses, []string{"create_note"},
			listing(1, "c2", "list_notes"),
			[]TargetStat{{Name: "create_note", Listing: ListingUnverified}}},
		{"pages and re-lists are a union", onResponses, []string{"list_notes", "delete_note", "create_note"},
			concat(listing(1, "c2", "list_notes"), listing(2, "", "delete_note"), listing(3, "", "other")),
			[]TargetStat{{Name: "list_notes", Listing: ListingListed}, {Name: "delete_note", Listing: ListingListed}, {Name: "create_note", Listing: ListingNotListed}}},
		{"a result without a tools array is not a page", onResponses, []string{"create_note"},
			[]step{{true, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`}, reply(1, `{}`)},
			[]TargetStat{{Name: "create_note", Listing: ListingUnverified}}},
		{"a client-prefixed name points at the listed one", onResponses, []string{"mcp__notes__create_note"},
			listing(1, "", "create_note"),
			[]TargetStat{{Name: "mcp__notes__create_note", Listing: ListingNotListed, ListedAs: "create_note"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewGremlinHandler([]gremlins.Gremlin{tt.g}, NewInjectionLog(), zerolog.Nop(), WithTargets(tt.targets))
			feed(t, h, tt.steps)
			assert.Equal(t, tt.want, h.Targets())
		})
	}
}

func TestGremlinHandler_UntargetedHasNoTargets(t *testing.T) {
	h := NewGremlinHandler(nil, NewInjectionLog(), zerolog.Nop())
	feed(t, h, listing(1, "", "create_note"))
	assert.Nil(t, h.Targets())
	assert.Nil(t, (*GremlinHandler)(nil).Targets())
}

func TestGremlinHandler_TargetCountsAreRaceFree(t *testing.T) {
	h := NewGremlinHandler(nil, NewInjectionLog(), zerolog.Nop(), createNote)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			msg, err := protocol.ParseMessage([]byte(call(i, "create_note").raw))
			assert.NoError(t, err)
			_, _ = h.HandleMessage(context.Background(), msg, &proxy.MessageContext{})
			_ = h.Targets()
		})
	}
	wg.Wait()
	assert.Equal(t, 50, h.Targets()[0].Called)
}

func TestResolveTargets(t *testing.T) {
	event := func(tool string, outcome models.ArenaOutcome) models.ArenaEvent {
		return models.ArenaEvent{Outcome: outcome, Details: json.RawMessage(fmt.Sprintf(`{"tool":%q}`, tool))}
	}
	events := []models.ArenaEvent{
		event("create_note", models.OutcomeSurvived),
		event("create_note", models.OutcomeUnmeasured),
		event("delete_note", models.OutcomeUnmeasured),
		{Outcome: models.OutcomeSurvived},
	}
	stats := []TargetStat{
		{Name: "list_notes"},
		{Name: "read_note", Called: 2},
		{Name: "delete_note", Called: 1, Injected: 1},
		{Name: "create_note", Called: 1, Injected: 1},
	}
	got := ResolveTargets(stats, events)
	require.Len(t, got, 4)
	assert.Equal(t, []TargetState{TargetNeverCalled, TargetNotInjected, TargetUnobserved, TargetScored},
		[]TargetState{got[0].State, got[1].State, got[2].State, got[3].State})
	assert.Equal(t, 1, got[3].Scored, "an unmeasured event is not a score")
	assert.Nil(t, ResolveTargets(nil, events))
}
