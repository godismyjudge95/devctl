package services

import (
	"sync"
)

// Registry holds the active set of service definitions.
// The base set comes from config.DefaultServices() at startup; additional
// definitions (e.g. PHP-FPM versions) can be added/removed at runtime via
// Register/Unregister.
type Registry struct {
	mu   sync.RWMutex
	defs []Definition
}

// NewRegistry creates an in-memory Registry from a slice of Definitions.
func NewRegistry(defs []Definition) *Registry {
	out := make([]Definition, len(defs))
	copy(out, defs)
	return &Registry{defs: out}
}

// Register adds or replaces a Definition in the registry.
// If a definition with the same ID already exists it is replaced.
func (r *Registry) Register(def Definition) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, d := range r.defs {
		if d.ID == def.ID {
			r.defs[i] = def
			return
		}
	}
	r.defs = append(r.defs, def)
}

// Unregister removes the definition with the given ID from the registry.
// It is a no-op if the ID is not found.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, d := range r.defs {
		if d.ID == id {
			r.defs = append(r.defs[:i], r.defs[i+1:]...)
			return
		}
	}
}

// All returns all loaded service definitions.
func (r *Registry) All() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Definition, len(r.defs))
	copy(out, r.defs)
	return out
}

// Get returns a definition by ID, or false if not found.
func (r *Registry) Get(id string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, d := range r.defs {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}
