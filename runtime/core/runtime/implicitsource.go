package runtime

import (
	"context"

	"github.com/juancavallotti/octo/types"
)

// implicitSource is the entry point for a flow that has no external source. It
// owns no resources: Start registers the flow's input channel under the flow name
// in the registry (making the flow callable by name) and Stop deregisters it. It
// never emits on its own — messages arrive only from direct invocation (the CLI)
// or a flow-ref block.
type implicitSource struct {
	name     string
	out      chan<- *types.Message
	registry *flowRegistry
}

// newImplicitSource builds an implicit source for the named flow emitting on out.
func newImplicitSource(name string, out chan<- *types.Message, registry *flowRegistry) *implicitSource {
	return &implicitSource{name: name, out: out, registry: registry}
}

// Start registers the flow's input channel so callers can reach it by name.
func (s *implicitSource) Start(_ context.Context) error {
	return s.registry.register(s.name, s.out)
}

// Stop deregisters the flow so it is no longer callable.
func (s *implicitSource) Stop(_ context.Context) error {
	s.registry.deregister(s.name)
	return nil
}
