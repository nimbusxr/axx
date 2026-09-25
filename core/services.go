package core

import (
	"fmt"
	"sync"
)

// Services is an ordered, per-scenario registry of named services (REST
// services, databases, brokers...). The first registered service is the
// default one used by steps that do not name a service.
type Services[S any] struct {
	kind  string // e.g. "Service", "Database service"
	empty string // message when no service is registered

	mu    sync.Mutex
	names []string
	items map[string]S
}

// NewServices creates a registry. kind labels errors (`<kind> "x" not set`);
// empty is the message used when none is registered (default "No <kind> set").
func NewServices[S any](kind, empty string) *Services[S] {
	if empty == "" {
		empty = "No " + lowerFirst(kind) + " set"
	}
	return &Services[S]{kind: kind, empty: empty, items: map[string]S{}}
}

// Add registers a service. Registering the same name twice is an error.
func (r *Services[S]) Add(name string, s S) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[name]; ok {
		return fmt.Errorf("%s %q already set", r.kind, name)
	}
	r.names = append(r.names, name)
	r.items[name] = s
	return nil
}

// Get returns the named service.
func (r *Services[S]) Get(name string) (S, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.items[name]
	if !ok {
		var zero S
		return zero, fmt.Errorf("%s %q not set", r.kind, name)
	}
	return s, nil
}

// Default returns the first registered service.
func (r *Services[S]) Default() (S, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.names) == 0 {
		var zero S
		return zero, fmt.Errorf("%s", r.empty)
	}
	return r.items[r.names[0]], nil
}

// Lookup returns the named service, or the default one when name is "".
func (r *Services[S]) Lookup(name string) (S, error) {
	if name == "" {
		return r.Default()
	}
	return r.Get(name)
}

// All returns services in registration order.
func (r *Services[S]) All() []S {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]S, 0, len(r.names))
	for _, n := range r.names {
		out = append(out, r.items[n])
	}
	return out
}

// Names returns service names in registration order.
func (r *Services[S]) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.names...)
}

// Len returns the number of registered services.
func (r *Services[S]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.names)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	if b[0] >= 'A' && b[0] <= 'Z' {
		b[0] += 'a' - 'A'
	}
	return string(b)
}
