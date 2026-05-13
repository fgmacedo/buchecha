package provider

import "sort"

// Registry maps provider names to Provider implementations. It is the
// single source of truth for which vendors are available in a run.
// Construct with NewRegistry; the zero value is invalid.
type Registry struct {
	byName map[string]Provider
}

// NewRegistry builds a Registry from the given providers. Each provider
// is indexed by its Name() return value. Duplicate names are silently
// overwritten by the later entry; callers should not register two
// providers with the same name.
func NewRegistry(providers ...Provider) *Registry {
	r := &Registry{byName: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		r.byName[p.Name()] = p
	}
	return r
}

// Get returns the Provider registered under name and true, or a nil
// Provider and false when name is not registered.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.byName[name]
	return p, ok
}

// Names returns the sorted list of registered provider names. The order
// is stable across calls; callers that display or iterate over providers
// get a deterministic sequence regardless of registration order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.byName))
	for name := range r.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
