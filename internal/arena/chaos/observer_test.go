package chaos

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memEvents struct {
	mu     sync.Mutex
	events []models.ArenaEvent
}

func (m *memEvents) RecordEvent(_ context.Context, e models.ArenaEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
	return nil
}

func (m *memEvents) all() []models.ArenaEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]models.ArenaEvent, len(m.events))
	copy(out, m.events)
	return out
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Unix(1700000000, 0).UTC()}
}
func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func callTool(id int64, tool string) *protocol.Message {
	params, _ := json.Marshal(map[string]any{"name": tool, "arguments": map[string]string{}})
	return &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(id),
			Method:  string(protocol.MCPMethodToolsCall),
			Params:  params,
		},
	}
}

func outgoing() *proxy.MessageContext {
	return &proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing}
}

func injectionOn(id string, call *protocol.Message) Injection {
	response := &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      call.Request.ID,
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"corrupted"}]}`),
		},
	}

	return Injection{
		ID:          id,
		GremlinName: "corruption",
		Direction:   models.DirectionIncoming,
		RequestID:   messageID(call),
		Method:      string(protocol.MCPMethodToolsCall),
		Tool:        toolName(call),
		InjectedAt:  time.Unix(1700000000, 0).UTC(),
		Original:    response,
	}
}

func newObs(t *testing.T, clk *fakeClock, events EventSink) *Observer {
	t.Helper()
	return NewObserver(events, zerolog.Nop(),
		WithWindow(30*time.Second),
		WithObserverClock(clk.now),
	)
}

func TestObserver_RetryIsSurvived(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	call := callTool(1, "search_contacts")
	o.RecordInjection(injectionOn("inj-1", call))

	_, err := o.HandleMessage(context.Background(), callTool(2, "search_contacts"), outgoing())
	require.NoError(t, err)

	events := ev.all()
	require.Len(t, events, 1)
	assert.Equal(t, models.OutcomeSurvived, events[0].Outcome)
	assert.Equal(t, 90, events[0].Score)
	assertReaction(t, events[0], ReactionRetried)
}

func TestObserver_DifferentToolIsAdapted(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))

	_, err := o.HandleMessage(context.Background(), callTool(2, "read_graph"), outgoing())
	require.NoError(t, err)

	events := ev.all()
	require.Len(t, events, 1)
	assert.Equal(t, models.OutcomeSurvived, events[0].Outcome)
	assert.Equal(t, 70, events[0].Score)
	assertReaction(t, events[0], ReactionAdapted)
}

func TestObserver_SilenceIsCrashed(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))

	cov := o.Finalize(context.Background(), true)

	events := ev.all()
	require.Len(t, events, 1)
	assert.Equal(t, models.OutcomeCrashed, events[0].Outcome)
	assert.Equal(t, 10, events[0].Score)
	assertReaction(t, events[0], ReactionSilent)
	assert.True(t, cov.Complete())
}

func TestObserver_UnrelatedTrafficIsContinued(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))

	listing := &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(2),
			Method:  string(protocol.MCPMethodToolsList),
		},
	}
	_, err := o.HandleMessage(context.Background(), listing, outgoing())
	require.NoError(t, err)
	assert.Empty(t, ev.all(), "unrelated traffic alone must not resolve the watch")

	cov := o.Finalize(context.Background(), true)

	events := ev.all()
	require.Len(t, events, 1)
	assert.Equal(t, models.OutcomeDegraded, events[0].Outcome)
	assertReaction(t, events[0], ReactionContinued)
	assert.True(t, cov.Complete())
}

func TestObserver_DeadlineResolvesAsSilent(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))
	clk.advance(31 * time.Second)

	_, err := o.HandleMessage(context.Background(), callTool(2, "search_contacts"), outgoing())
	require.NoError(t, err)

	events := ev.all()
	require.Len(t, events, 1)
	assertReaction(t, events[0], ReactionSilent,
		"a retry arriving after the window must not count as a reaction")
}

func TestObserver_ReactionInsideWindowCounts(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))
	clk.advance(29 * time.Second)

	_, err := o.HandleMessage(context.Background(), callTool(2, "search_contacts"), outgoing())
	require.NoError(t, err)

	require.Len(t, ev.all(), 1)
	assertReaction(t, ev.all()[0], ReactionRetried)
}

func TestObserver_IncomingResponseIsNotAReaction(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))

	_, err := o.HandleMessage(context.Background(), toolResult(2, `{"ok":true}`), incoming())
	require.NoError(t, err)

	assert.Empty(t, ev.all(), "a server response must not resolve a watch")
	assert.Equal(t, 1, o.Coverage().Unresolved)
}

func TestObserver_SameRequestIDIsNotAReaction(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	call := callTool(1, "search_contacts")
	o.RecordInjection(injectionOn("inj-1", call))

	_, err := o.HandleMessage(context.Background(), callTool(1, "search_contacts"), outgoing())
	require.NoError(t, err)

	assert.Empty(t, ev.all())
}

func TestObserver_CancelledSessionLeavesInjectionUnobserved(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))

	cov := o.Finalize(context.Background(), false)

	assert.Empty(t, ev.all(), "an unobserved injection must not be scored")
	assert.Equal(t, 1, cov.Injected)
	assert.Equal(t, 0, cov.Observed)
	assert.Equal(t, 1, cov.Unresolved)
	assert.False(t, cov.Complete())
}

func TestObserver_NoInjectionsIsNotComplete(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	cov := o.Finalize(context.Background(), true)

	assert.Empty(t, ev.all())
	assert.Equal(t, Coverage{}, cov)
	assert.False(t, cov.Complete(), "zero injections can never be complete coverage")
}

func TestObserver_CoverageCountsEveryInjection(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)

	for i := int64(1); i <= 3; i++ {
		o.RecordInjection(injectionOn(fmt.Sprintf("inj-%d", i), callTool(i, "search_contacts")))
	}

	_, err := o.HandleMessage(context.Background(), callTool(99, "search_contacts"), outgoing())
	require.NoError(t, err)

	cov := o.Finalize(context.Background(), true)
	assert.Equal(t, 3, cov.Injected)
	assert.Equal(t, 3, cov.Observed)
	assert.Zero(t, cov.Unresolved)
	assert.True(t, cov.Complete())
	assert.Len(t, ev.all(), 3)
}

func TestCoverage_String(t *testing.T) {
	c := Coverage{Injected: 2, Observed: 1, Unresolved: 1}
	assert.Equal(t, "injected=2 observed=1 unresolved=1 complete=false", c.String())
}

func TestObserver_ResolutionIsDeterministic(t *testing.T) {
	run := func() []string {
		clk, ev := newClock(), &memEvents{}
		o := newObs(t, clk, ev)
		for i := int64(1); i <= 4; i++ {
			o.RecordInjection(injectionOn(fmt.Sprintf("inj-%d", i), callTool(i, "search_contacts")))
		}
		_, _ = o.HandleMessage(context.Background(), callTool(10, "search_contacts"), outgoing())
		_, _ = o.HandleMessage(context.Background(), callTool(11, "read_graph"), outgoing())
		o.Finalize(context.Background(), true)

		all := ev.all()
		out := make([]string, 0, len(all))
		for _, e := range all {
			var d map[string]string
			_ = json.Unmarshal(e.Details, &d)
			out = append(out, e.ID+":"+d["reaction"])
		}
		return out
	}
	assert.Equal(t, run(), run())
}

func TestObserver_NeverModifiesMessages(t *testing.T) {
	clk, ev := newClock(), &memEvents{}
	o := newObs(t, clk, ev)
	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))

	msg := callTool(2, "search_contacts")
	dec, err := o.HandleMessage(context.Background(), msg, outgoing())

	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionSkip, dec.Action, "the observer must never alter traffic")
	assert.Nil(t, dec.ModifiedMessage)
}

func TestObserver_NilEventSinkIsSafe(t *testing.T) {
	clk := newClock()
	o := newObs(t, clk, nil)
	o.RecordInjection(injectionOn("inj-1", callTool(1, "search_contacts")))
	cov := o.Finalize(context.Background(), true)
	assert.True(t, cov.Complete())
}

func TestToolName(t *testing.T) {
	tests := []struct {
		name string
		msg  *protocol.Message
		want string
	}{
		{"tools/call", callTool(1, "search_contacts"), "search_contacts"},
		{"tools/list has no tool", &protocol.Message{
			Type:    protocol.MessageTypeRequest,
			Request: &protocol.JSONRPCRequest{Method: string(protocol.MCPMethodToolsList)},
		}, ""},
		{"response has no tool", toolResult(1, `{}`), ""},
		{"nil", nil, ""},
		{"malformed params", &protocol.Message{
			Type: protocol.MessageTypeRequest,
			Request: &protocol.JSONRPCRequest{
				Method: string(protocol.MCPMethodToolsCall),
				Params: json.RawMessage(`not json`),
			},
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, toolName(tt.msg))
		})
	}
}

func TestObserverWithHandler_OutcomeFollowsAgentBehaviour(t *testing.T) {
	cases := []struct {
		name        string
		reactWith   *protocol.Message
		wantOutcome models.ArenaOutcome
	}{
		{"agent retries", callTool(2, "search_contacts"), models.OutcomeSurvived},
		{"agent switches tool", callTool(2, "read_graph"), models.OutcomeSurvived},
		{"agent does nothing", nil, models.OutcomeCrashed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk, ev := newClock(), &memEvents{}
			obs := newObs(t, clk, ev)

			pipeline := proxy.NewPipeline(zerolog.Nop())
			pipeline.RegisterHandler(newHandler(obs, corruptsResponsesOnly("corruption")))
			pipeline.RegisterHandler(obs)

			ctx := context.Background()
			call := callTool(1, "search_contacts")

			_, _, err := pipeline.Process(ctx, call, outgoing())
			require.NoError(t, err)
			_, _, err = pipeline.Process(ctx, toolResult(1, `{"pristine":true}`), incoming())
			require.NoError(t, err)

			if tc.reactWith != nil {
				_, _, err = pipeline.Process(ctx, tc.reactWith, outgoing())
				require.NoError(t, err)
			}
			cov := obs.Finalize(ctx, true)

			events := ev.all()
			require.NotEmpty(t, events, "the injection must have produced an event")
			assert.Equal(t, tc.wantOutcome, events[len(events)-1].Outcome)
			assert.True(t, cov.Complete(), "coverage: %s", cov)
		})
	}
}

func TestObserverWithHandler_SameGremlinsDifferentAgentsDifferentOutcomes(t *testing.T) {
	outcomeFor := func(react bool) models.ArenaOutcome {
		clk, ev := newClock(), &memEvents{}
		obs := newObs(t, clk, ev)

		pipeline := proxy.NewPipeline(zerolog.Nop())
		pipeline.RegisterHandler(newHandler(obs, corruptsResponsesOnly("corruption")))
		pipeline.RegisterHandler(obs)

		ctx := context.Background()
		_, _, _ = pipeline.Process(ctx, callTool(1, "search_contacts"), outgoing())
		_, _, _ = pipeline.Process(ctx, toolResult(1, `{}`), incoming())
		if react {
			_, _, _ = pipeline.Process(ctx, callTool(2, "search_contacts"), outgoing())
		}
		obs.Finalize(ctx, true)

		all := ev.all()
		require.NotEmpty(t, all)
		return all[len(all)-1].Outcome
	}

	robust := outcomeFor(true)
	fragile := outcomeFor(false)

	assert.Equal(t, models.OutcomeSurvived, robust)
	assert.Equal(t, models.OutcomeCrashed, fragile)
	assert.NotEqual(t, robust, fragile,
		"identical gremlins must yield different outcomes for different agent behaviour")
}

func assertReaction(t *testing.T, e models.ArenaEvent, want Reaction, msgAndArgs ...any) {
	t.Helper()
	var d map[string]string
	require.NoError(t, json.Unmarshal(e.Details, &d))
	assert.Equal(t, string(want), d["reaction"], msgAndArgs...)
}
