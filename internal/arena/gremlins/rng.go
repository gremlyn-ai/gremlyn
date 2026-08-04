package gremlins

import (
	"hash/fnv"
	"math/rand"
	"sync"
)

// DefaultSeed is the seed used when a caller does not supply one.
//
// Gremlins are reproducible by default on purpose. A chaos tool whose default is
// irreproducible produces scores that cannot be compared to each other, to a
// previous run, or to another agent — which makes the score decorative. Callers
// that genuinely want variation pass an explicit seed (a timestamp, a run id).
const DefaultSeed int64 = 1

// rng is a seeded random source that is safe for concurrent use.
//
// The proxy is full duplex: the client→server and server→client loops run in
// separate goroutines and can reach the same gremlin at the same time. The global
// math/rand source is internally locked, but a *rand.Rand is not, so switching to
// a seeded source means taking that lock ourselves.
type rng struct {
	mu sync.Mutex
	r  *rand.Rand
}

// newRNG builds a random source for one gremlin.
//
// The seed is mixed with the gremlin's name so each gremlin draws from its own
// stream. That means enabling or disabling one gremlin does not shift another's
// sequence, so two sessions with different gremlin sets remain comparable on the
// gremlins they share.
func newRNG(seed int64, name string) *rng {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return &rng{r: rand.New(rand.NewSource(seed ^ int64(h.Sum64())))} //nolint:gosec // not cryptographic: reproducibility is the requirement
}

// Float64 returns a pseudo-random number in [0.0, 1.0).
func (g *rng) Float64() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.r.Float64()
}

// Intn returns a pseudo-random int in [0, n). It returns 0 when n <= 0, so a
// misconfigured bound cannot panic inside the proxy hot path.
func (g *rng) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.r.Intn(n)
}

// Option configures a gremlin at construction.
type Option func(*options)

type options struct {
	seed int64
}

// WithSeed sets the gremlin's random seed, making its injection pattern
// reproducible for a given message sequence.
func WithSeed(seed int64) Option {
	return func(o *options) { o.seed = seed }
}

func applyOptions(opts []Option) options {
	o := options{seed: DefaultSeed}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
