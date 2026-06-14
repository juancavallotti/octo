package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/juancavallotti/eip-go/types"
)

type Connector interface {
	Start(ctx context.Context, config types.ConnectorConfig) error
	Stop(ctx context.Context) error
}

type Factory func() Connector

type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

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

func (r *Registry) MustRegister(name string, factory Factory) {
	if err := r.Register(name, factory); err != nil {
		panic(err)
	}
}

func (r *Registry) New(name string) (Connector, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("connector %q not registered", name)
	}
	return factory(), nil
}

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

func DefaultRegistry() *Registry {
	return defaultRegistry
}

func RegisterConnector(name string, factory Factory) error {
	return defaultRegistry.Register(name, factory)
}

func MustRegisterConnector(name string, factory Factory) {
	defaultRegistry.MustRegister(name, factory)
}
