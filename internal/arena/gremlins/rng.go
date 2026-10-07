package gremlins

import (
	"hash/fnv"
	"math/rand"
	"sync"
)

const DefaultSeed int64 = 1

type rng struct {
	mu sync.Mutex
	r  *rand.Rand
}

func newRNG(seed int64, name string) *rng {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	return &rng{r: rand.New(rand.NewSource(seed ^ int64(h.Sum64())))}
}

func (g *rng) Float64() float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.r.Float64()
}

func (g *rng) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.r.Intn(n)
}

type Option func(*options)

type options struct {
	seed int64
}

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
