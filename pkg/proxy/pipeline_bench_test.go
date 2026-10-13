package proxy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/rs/zerolog"
)

func BenchmarkPipeline_ToolCallRoundTrip(b *testing.B) {
	pipeline := NewPipeline(zerolog.Nop())
	defer pipeline.Close()
	req := &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: "2.0", ID: protocol.NewIntID(7), Method: "tools/call",
			Params: json.RawMessage(`{"name":"read","arguments":{}}`),
		},
	}
	resp := &protocol.Message{
		Type: protocol.MessageTypeResponse,
		Response: &protocol.JSONRPCResponse{
			JSONRPC: "2.0", ID: protocol.NewIntID(7), Result: json.RawMessage(`{"content":[]}`),
		},
	}
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _, _ = pipeline.Process(ctx, req, &MessageContext{Direction: models.DirectionOutgoing})
		_, _, _ = pipeline.Process(ctx, resp, &MessageContext{Direction: models.DirectionIncoming})
	}
}
