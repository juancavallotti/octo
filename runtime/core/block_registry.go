package core

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/juancavallotti/octo/runtime/types"
)

// BlockRegistry holds block factories keyed by block type. It is the one place
// a flow's block types resolve: leaf blocks and blocks that embed sub-flows are
// registered alike, and the engine dispatches every authored block through it.
type BlockRegistry struct {
	mu        sync.RWMutex
	factories map[string]BlockFactory
}

// NewBlockRegistry returns an empty block registry.
func NewBlockRegistry() *BlockRegistry {
	return &BlockRegistry{factories: make(map[string]BlockFactory)}
}

// Register adds a factory under name, failing if name is empty or already taken.
func (r *BlockRegistry) Register(name string, factory BlockFactory) error {
	if name == "" {
		return errors.New("block name is required")
	}
	if factory == nil {
		return errors.New("block factory is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("block %q already registered", name)
	}

	r.factories[name] = factory
	return nil
}

// MustRegister is like Register but panics if registration fails.
func (r *BlockRegistry) MustRegister(name string, factory BlockFactory) {
	if err := r.Register(name, factory); err != nil {
		panic(err)
	}
}

// New constructs a processor for the registered block type.
//
//nolint:ireturn // a factory intentionally returns the MessageProcessor interface
func (r *BlockRegistry) New(name string, settings types.Settings, deps BlockDeps) (MessageProcessor, error) {
	r.mu.RLock()
	factory, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("block %q not registered", name)
	}
	return factory(settings, deps)
}

// Factory returns the factory registered under name, if any. It is how a
// registry assembled for one purpose — a test's, with its own throwaway blocks —
// can carry over the blocks another registry holds.
func (r *BlockRegistry) Factory(name string) (BlockFactory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, ok := r.factories[name]
	return factory, ok
}

// Names returns the registered block type names in sorted order.
func (r *BlockRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var defaultBlockRegistry = NewBlockRegistry()

// DefaultBlockRegistry returns the process-wide block registry.
func DefaultBlockRegistry() *BlockRegistry {
	return defaultBlockRegistry
}

// RegisterBlock registers a block factory on the default registry.
func RegisterBlock(name string, factory BlockFactory) error {
	return defaultBlockRegistry.Register(name, factory)
}

// MustRegisterBlock registers a block factory on the default registry, panicking
// on failure.
func MustRegisterBlock(name string, factory BlockFactory) {
	defaultBlockRegistry.MustRegister(name, factory)
}
