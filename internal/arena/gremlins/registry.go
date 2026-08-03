package gremlins

import "sync"

// Registry holds all available gremlins and provides lookup by name.
type Registry struct {
	mu       sync.RWMutex
	gremlins map[string]Gremlin
}

// NewRegistry creates an empty gremlin registry.
func NewRegistry() *Registry {
	return &Registry{
		gremlins: make(map[string]Gremlin),
	}
}

// Register adds a gremlin to the registry.
func (r *Registry) Register(g Gremlin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gremlins[g.Name()] = g
}

// Get returns a gremlin by name.
func (r *Registry) Get(name string) (Gremlin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	g, ok := r.gremlins[name]
	return g, ok
}

// List returns all registered gremlins.
func (r *Registry) List() []Gremlin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Gremlin, 0, len(r.gremlins))
	for _, g := range r.gremlins {
		list = append(list, g)
	}
	return list
}

// Names returns the names of all registered gremlins.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.gremlins))
	for name := range r.gremlins {
		names = append(names, name)
	}
	return names
}
