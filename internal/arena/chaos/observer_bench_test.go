package chaos

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func BenchmarkObserver_AgentCallWithNothingPending(b *testing.B) {
	o := NewObserver(&memEvents{}, zerolog.Nop(), WithWindow(30*time.Second), WithObserverClock(newClock().now))
	msg, mctx := callTool(1, "read_graph"), outgoing()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = o.HandleMessage(ctx, msg, mctx)
	}
}

func BenchmarkObserver_ResultWithAPendingInjection(b *testing.B) {
	o := NewObserver(nil, zerolog.Nop(), WithWindow(time.Hour), WithObserverClock(newClock().now))
	call := callTool(1, "read_graph")
	o.RecordInjection(injectionOn("inj", call))
	msg, mctx := resultFor(9, `{"content":[{"type":"text","text":"ok"}]}`), incomingFor(callTool(9, "read_graph"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = o.HandleMessage(ctx, msg, mctx)
	}
}
