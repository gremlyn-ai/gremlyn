package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/gremlyn-ai/gremlyn/pkg/protocol"
	"github.com/gremlyn-ai/gremlyn/pkg/proxy"
	"github.com/rs/zerolog"
)

// RateLimiter implements a sliding window rate limiter as a pipeline Handler.
type RateLimiter struct {
	maxCount int
	window   time.Duration
	perAgent bool
	logger   zerolog.Logger

	mu      sync.Mutex
	windows map[string]*slidingWindow
}

type slidingWindow struct {
	timestamps []time.Time
}

// NewRateLimiter creates a new RateLimiter.
func NewRateLimiter(maxCount int, window time.Duration, perAgent bool, logger zerolog.Logger) *RateLimiter {
	return &RateLimiter{
		maxCount: maxCount,
		window:   window,
		perAgent: perAgent,
		logger:   logger,
		windows:  make(map[string]*slidingWindow),
	}
}

// Name returns the handler name.
func (r *RateLimiter) Name() string { return "shield-rate-limiter" }

// Priority returns 5 — rate limiting runs before policy.
func (r *RateLimiter) Priority() int { return 5 }

// Direction returns outgoing — rate limit outgoing requests only.
func (r *RateLimiter) Direction() models.Direction { return models.DirectionOutgoing }

// HandleMessage checks whether the request exceeds the rate limit.
func (r *RateLimiter) HandleMessage(_ context.Context, _ *protocol.Message, mctx *proxy.MessageContext) (*proxy.Decision, error) {
	key := mctx.ServerName
	if r.perAgent {
		key = "agent:" + mctx.ServerName
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	w, ok := r.windows[key]
	if !ok {
		w = &slidingWindow{}
		r.windows[key] = w
	}

	now := time.Now()
	cutoff := now.Add(-r.window)

	// Prune expired entries.
	valid := make([]time.Time, 0, len(w.timestamps))
	for _, ts := range w.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	w.timestamps = valid

	if len(w.timestamps) >= r.maxCount {
		r.logger.Warn().Str("key", key).Int("count", len(w.timestamps)).Msg("rate limit exceeded")
		return &proxy.Decision{
			Action: proxy.DecisionBlock,
			Reason: fmt.Sprintf("rate limit exceeded: %d/%s", r.maxCount, r.window),
		}, nil
	}

	w.timestamps = append(w.timestamps, now)
	return &proxy.Decision{Action: proxy.DecisionSkip}, nil
}
