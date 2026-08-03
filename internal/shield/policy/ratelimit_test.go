package policy

import (
	"context"
	"testing"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(5, time.Minute, false, zerolog.Nop())
	msg := &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(1),
			Method:  "tools/call",
		},
	}
	mctx := &proxy.MessageContext{
		ServerName: "test",
		Direction:  models.DirectionOutgoing,
		Timestamp:  time.Now(),
	}

	for i := 0; i < 5; i++ {
		decision, err := rl.HandleMessage(context.Background(), msg, mctx)
		require.NoError(t, err)
		assert.Equal(t, proxy.DecisionSkip, decision.Action, "request %d should be allowed", i)
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute, false, zerolog.Nop())
	msg := &protocol.Message{
		Type: protocol.MessageTypeRequest,
		Request: &protocol.JSONRPCRequest{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      protocol.NewIntID(1),
			Method:  "tools/call",
		},
	}
	mctx := &proxy.MessageContext{
		ServerName: "test",
		Direction:  models.DirectionOutgoing,
		Timestamp:  time.Now(),
	}

	// Fill up the limit.
	for i := 0; i < 3; i++ {
		decision, err := rl.HandleMessage(context.Background(), msg, mctx)
		require.NoError(t, err)
		assert.Equal(t, proxy.DecisionSkip, decision.Action)
	}

	// Fourth request should be blocked.
	decision, err := rl.HandleMessage(context.Background(), msg, mctx)
	require.NoError(t, err)
	assert.Equal(t, proxy.DecisionBlock, decision.Action)
	assert.Contains(t, decision.Reason, "rate limit exceeded")
}

func TestRateLimiter_Properties(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute, true, zerolog.Nop())
	assert.Equal(t, "shield-rate-limiter", rl.Name())
	assert.Equal(t, 5, rl.Priority())
	assert.Equal(t, models.DirectionOutgoing, rl.Direction())
}
