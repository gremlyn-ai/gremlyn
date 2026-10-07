package gremlins

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
)

type LoopGremlin struct {
	MaxLoops    int     `json:"max_loops"`
	Message     string  `json:"message"`
	Probability float64 `json:"probability"`
	mu          sync.Mutex
	loopCount   int
	rng         *rng
}

func NewLoopGremlin(maxLoops int, message string, probability float64, opts ...Option) *LoopGremlin {
	o := applyOptions(opts)
	if message == "" {
		message = "Action completed but needs verification. Please retry."
	}
	return &LoopGremlin{
		MaxLoops:    maxLoops,
		Message:     message,
		Probability: probability,
		rng:         newRNG(o.seed, "loop"),
	}
}
func (g *LoopGremlin) Name() string { return "loop" }
func (g *LoopGremlin) Description() string {
	return "Replaces responses with retry messages to test circuit breaker implementation"
}

func (g *LoopGremlin) Inject(_ context.Context, msg *protocol.Message) (*protocol.Message, bool, error) {
	if msg == nil {
		return nil, false, nil
	}

	if msg.Response == nil {
		return msg, false, nil
	}

	if g.rng.Float64() >= g.Probability {
		return msg, false, nil
	}

	g.mu.Lock()
	g.loopCount++
	count := g.loopCount
	g.mu.Unlock()

	if count > g.MaxLoops {
		return msg, false, nil
	}

	modified := *msg
	modResp := *msg.Response
	retryResult, _ := json.Marshal(map[string]string{
		"status":  "needs_verification",
		"message": g.Message,
	})
	modResp.Result = retryResult
	modified.Response = &modResp

	return &modified, true, nil
}

func (g *LoopGremlin) LoopCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.loopCount
}

func (g *LoopGremlin) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.loopCount = 0
}
