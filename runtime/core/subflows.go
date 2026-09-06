package core

import (
	"errors"

	"github.com/juancavallotti/octo/runtime/types"
)

// SubFlowBuilder builds the nested chains a block declares. It is the seam that
// lets a block outside the engine own a sub-flow: the block reads a FlowConfig or
// a block list out of its own settings, and hands it here to be turned into a
// runnable processor positioned under the block's own address.
//
// It is positioned at the block being built. Every chain it returns is addressed
// as `<block>[<branch>].<step>`, where the branch is the name the block passes
// in — which must be the json name of the settings field the chain came from, so
// the address resolver, which reads that field from the block's schema, and the
// engine, which mints the path, agree on how the chain is spelled.
//
// A chain it builds is a sub-flow: it ends at the block that owns it, which then
// post-processes the result. It may not declare a source, workers, a pool, or an
// error path, and a block inside it that needs a continuation fails to build.
type SubFlowBuilder interface {
	// Branch builds the chain under a slot addressed by its own name: `then`,
	// `body`, `onReject`. It fails on an empty or absent chain — a block whose
	// slot is optional checks for that before calling.
	Branch(name string, flow types.FlowConfig) (MessageProcessor, error)
	// Member builds one entry of a list-valued slot — a fork's branch, a switch's
	// case, an agent's tool. It is addressed by its own name, or by its index in
	// the slot when it has none (see MemberBranch).
	Member(name string, index int, flow types.FlowConfig) (MessageProcessor, error)
	// Root reports whether the block being built sits in a flow's own process or
	// error chain rather than inside another block's slot. Only a root chain has a
	// meaningful "rest of the flow", which is what a block that takes the flow
	// over (see Continuation) requires.
	Root() bool
}

// Scheduler runs work on the flow's shared worker pool. A block that scatters
// work — a fork running its branches — submits through it rather than spawning
// goroutines of its own, so a flow's concurrency stays bounded by the pool it
// was configured with.
//
// Submit does not block. A saturated pool is a sizing mistake, and the runtime's
// pool says so by panicking rather than by silently serializing the work.
type Scheduler interface {
	Submit(task func())
}

// SchedulerFunc adapts a function to the Scheduler interface. Tests use it to
// run submitted work on a goroutine, or inline.
type SchedulerFunc func(task func())

// Submit runs task through the function.
func (f SchedulerFunc) Submit(task func()) { f(task) }

// ErrNoSubFlows is what a block reports when it needs to build a sub-flow and
// was built outside the engine: nothing in BlockDeps can construct one.
var ErrNoSubFlows = errors.New("block declares a sub-flow but was built without a flow builder")

// ErrNoScheduler is what a block reports when it needs the flow's worker pool and
// was built outside the engine.
var ErrNoScheduler = errors.New("block schedules concurrent work but was built without a scheduler")

// SubFlowsOf returns the sub-flow builder a block was handed, or ErrNoSubFlows
// when it has none. A block with a sub-flow slot calls this first, so a build
// outside the engine fails with a sentence rather than a nil dereference.
//
//nolint:ireturn // the seam is the interface; there is no concrete type to return
func SubFlowsOf(deps BlockDeps) (SubFlowBuilder, error) {
	if deps.SubFlows == nil {
		return nil, ErrNoSubFlows
	}
	return deps.SubFlows, nil
}
