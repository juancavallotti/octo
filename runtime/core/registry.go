package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/juancavallotti/octo/types"
)

// Connector is a runtime component that can be started and stopped.
type Connector interface {
	Start(ctx context.Context, config types.ConnectorConfig) error
	Stop(ctx context.Context) error
}

// Factory constructs a new Connector instance.
type Factory func() Connector

// Registry holds connector factories keyed by connector type.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry returns an empty connector registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Register adds a factory under name, failing if name is empty or already taken.
func (r *Registry) Register(name string, factory Factory) error {
	if name == "" {
		return errors.New("connector name is required")
	}
	if factory == nil {
		return errors.New("connector factory is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("connector %q already registered", name)
	}

	r.factories[name] = factory
	return nil
}

// MustRegister is like Register but panics if registration fails.
func (r *Registry) MustRegister(name string, factory Factory) {
	if err := r.Register(name, factory); err != nil {
		panic(err)
	}
}

// New constructs a Connector for the registered type name.
//
//nolint:ireturn // a factory intentionally returns the Connector interface
func (r *Registry) New(name string) (Connector, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("connector %q not registered", name)
	}
	return factory(), nil
}

// Has reports whether a connector type is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[name]
	return ok
}

// Names returns the registered connector type names in sorted order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var defaultRegistry = NewRegistry()

// DefaultRegistry returns the process-wide connector registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// RegisterConnector registers a factory on the default registry.
func RegisterConnector(name string, factory Factory) error {
	return defaultRegistry.Register(name, factory)
}

// MustRegisterConnector registers a factory on the default registry, panicking on failure.
func MustRegisterConnector(name string, factory Factory) {
	defaultRegistry.MustRegister(name, factory)
}
