package chaos

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func retryFixture(t *testing.T, gremlinName, tool string) (*Observer, *proxy.Pipeline, *fakeClock, *memEvents) {
	t.Helper()
	clk := &fakeClock{t: time.Unix(1700000000, 0).UTC()}
	events := &memEvents{}
	obs := NewObserver(events, zerolog.Nop(),
		WithWindow(30*time.Second),
		WithObserverClock(clk.now),
	)
	gs, err := BuildGremlins([]string{gremlinName}, 1, IntensityCertain)
	require.NoError(t, err)

	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(NewGremlinHandler(gs, obs, zerolog.Nop()))
	pipeline.RegisterHandler(obs)

	ctx := context.Background()

	call := toolCallMsg(1, tool)
	_, _, err = pipeline.Process(ctx, call,
		&proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
	require.NoError(t, err)

	_, _, err = pipeline.Process(ctx, toolResultMsg(1),
		&proxy.MessageContext{ServerName: "test", Direction: models.DirectionIncoming})
	require.NoError(t, err)

	require.Equal(t, 1, obs.Coverage().Injected,
		"the gremlin should have injected on the response")

	return obs, pipeline, clk, events
}

func toolCallMsg(id int64, tool string) *protocol.Message {
	return &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(id),
			Method:  string(protocol.MCPMethodToolsCall),
			Params:  json.RawMessage(`{"name":"` + tool + `","arguments":{"q":"x"}}`),
		},
	}
}

func toolResultMsg(id int64) *protocol.Message {
	return &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(id),
			Result:  json.RawMessage(`{"content":[{"type":"text","text":"a result with some fields"}]}`),
		},
	}
}

func TestObserver_RetryIsReachableForResponseSideGremlins(t *testing.T) {
	tests := []struct {
		gremlin   string
		wantScore int
		wantOut   models.ArenaOutcome
		why       string
	}{
		{"corruption", 90, models.OutcomeSurvived, "noticed the result was wrong and tried again"},
		{"latency", 40, models.OutcomeDegraded, "re-called a tool whose answer was on its way"},
		{"overflow", 90, models.OutcomeSurvived, "same"},
		{"timeout", 90, models.OutcomeSurvived, "same"},
		{"loop", 10, models.OutcomeCrashed, "walked into the loop the gremlin invited"},
		{"injection", 70, models.OutcomeSurvived, "distrusted the payload instead of obeying it"},
		{"identity", 70, models.OutcomeSurvived, "same"},
	}

	for _, tt := range tests {
		t.Run(tt.gremlin, func(t *testing.T) {
			obs, pipeline, clk, events := retryFixture(t, tt.gremlin, "search_contacts")

			_, _, err := pipeline.Process(context.Background(), toolCallMsg(2, "search_contacts"),
				&proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
			require.NoError(t, err)

			clk.advance(time.Second)
			cov := obs.Finalize(context.Background(), true)

			require.Equal(t, 1, cov.Observed,
				"the retry must resolve the injection — this was unreachable for every response-side gremlin")
			assert.Zero(t, cov.Unresolved)

			got := events.all()
			require.Len(t, got, 1)
			assert.Equal(t, tt.wantOut, got[0].Outcome, tt.why)
			assert.Equal(t, tt.wantScore, got[0].Score, tt.why)
		})
	}
}

func TestObserver_RetryOutscoresReachingForAnotherTool(t *testing.T) {
	retried := func() int {
		obs, pipeline, clk, events := retryFixture(t, "corruption", "search_contacts")
		_, _, err := pipeline.Process(context.Background(), toolCallMsg(2, "search_contacts"),
			&proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
		require.NoError(t, err)
		clk.advance(time.Second)
		obs.Finalize(context.Background(), true)
		return events.all()[0].Score
	}()

	adapted := func() int {
		obs, pipeline, clk, events := retryFixture(t, "corruption", "search_contacts")
		_, _, err := pipeline.Process(context.Background(), toolCallMsg(2, "a_different_tool"),
			&proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
		require.NoError(t, err)
		clk.advance(time.Second)
		obs.Finalize(context.Background(), true)
		return events.all()[0].Score
	}()

	assert.Equal(t, 90, retried)
	assert.Equal(t, 70, adapted)
	assert.Greater(t, retried, adapted,
		"retrying the failed tool and reaching for a different one must not score the same")
}

func TestObserver_RetryStillWorksForRequestSideGremlin(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1700000000, 0).UTC()}
	events := &memEvents{}
	obs := NewObserver(events, zerolog.Nop(),
		WithWindow(30*time.Second), WithObserverClock(clk.now))
	gs, err := BuildGremlins([]string{"hallucination"}, 1, IntensityCertain)
	require.NoError(t, err)

	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(NewGremlinHandler(gs, obs, zerolog.Nop()))
	pipeline.RegisterHandler(obs)

	ctx := context.Background()
	_, _, err = pipeline.Process(ctx, toolCallMsg(1, "search_contacts"),
		&proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
	require.NoError(t, err)
	require.Equal(t, 1, obs.Coverage().Injected)

	_, _, err = pipeline.Process(ctx, toolCallMsg(2, "search_contacts"),
		&proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
	require.NoError(t, err)

	clk.advance(time.Second)
	obs.Finalize(ctx, true)

	got := events.all()

	require.GreaterOrEqual(t, len(got), 1)
	assert.Equal(t, 90, got[0].Score,
		"the same-tool retry must still score 90 for the request-side gremlin")
	assert.Equal(t, models.OutcomeSurvived, got[0].Outcome)
}

func TestGremlinHandler_RecordsTheCorrelatedTool(t *testing.T) {
	sink := &recordingSink{}
	gs, err := BuildGremlins([]string{"corruption"}, 1, IntensityCertain)
	require.NoError(t, err)

	pipeline := proxy.NewPipeline(zerolog.Nop())
	pipeline.RegisterHandler(NewGremlinHandler(gs, sink, zerolog.Nop()))

	ctx := context.Background()
	_, _, err = pipeline.Process(ctx, toolCallMsg(7, "fetch_invoice"),
		&proxy.MessageContext{ServerName: "test", Direction: models.DirectionOutgoing})
	require.NoError(t, err)
	_, _, err = pipeline.Process(ctx, toolResultMsg(7),
		&proxy.MessageContext{ServerName: "test", Direction: models.DirectionIncoming})
	require.NoError(t, err)

	require.Len(t, sink.injections, 1)
	assert.Equal(t, "fetch_invoice", sink.injections[0].Tool,
		"a response-side injection must carry the tool from the correlated request")
	assert.Equal(t, string(protocol.MCPMethodToolsCall), sink.injections[0].Method)
}

type recordingSink struct {
	injections []Injection
}

func (s *recordingSink) RecordInjection(inj Injection) {
	s.injections = append(s.injections, inj)
}
