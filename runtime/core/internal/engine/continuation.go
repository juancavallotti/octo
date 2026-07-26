// Flow continuations: the machinery behind a block that takes over execution
// instead of returning the next message. See core.Continuation for what the
// capability means and why it exists; this file is the engine half — carving the
// rest of a chain out of an assembled flow and handing it to the block in front
// of it.
package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// errNoResume is returned by a Resume on a flow the runtime never installed a
// resume hook on. It means the block took over work it cannot schedule, so
// failing is the only honest outcome — silently dropping it would look like the
// message simply vanished.
var errNoResume = errors.New("flow has no resume hook: this flow was not built by the runtime")

// wireContinuations hands each block that takes over the flow the chain that
// follows it. It runs once per root flow, after the blocks are assembled, and is
// what makes a continuation a build-time constant: the same object serves every
// invocation, so a block may hold one and fire it long after the message that
// gave it work has gone.
//
// Only a root chain is wired. A composite's sub-flow ends at the composite,
// which post-processes the result, so a block inside one gets no continuation
// and fails its own build instead.
func (f *Flow) wireContinuations() error {
	last := len(f.Blocks) - 1
	for i := range f.Blocks {
		aware, ok := f.Blocks[i].Processor.(core.ContinuationAware)
		if !ok {
			continue
		}
		if i == last {
			// The tail would be empty, so every message it took over would be
			// scheduled through nothing. That is always a config mistake, and a
			// build error is a far better way to learn it than an idle flow.
			return fmt.Errorf(
				"block %q continues the flow but is the last block in the chain: nothing would run after it",
				blockLabel(f.Blocks[i].Type, f.Blocks[i].Name))
		}
		aware.SetContinuation(&flowContinuation{owner: f, tail: f.tailFrom(i + 1)})
	}
	return nil
}

// tailFrom returns the flow made of the blocks from i onwards. It shares the
// backing array rather than copying: the blocks are already built, and a tail is
// a view of the same chain, not a second one. Their Paths come along unchanged,
// so a resumed tail addresses exactly as it does in place — spies, breakpoints
// and mocks all keep working across a takeover.
func (f *Flow) tailFrom(i int) *Flow {
	return &Flow{
		Name:     f.Name,
		Blocks:   f.Blocks[i:],
		flowName: f.flowName,
		events:   f.events,
	}
}

// flowContinuation is one block's handle on the rest of its flow.
type flowContinuation struct {
	// owner is the flow the tail was carved from. The resume hook is read off it
	// at resume time rather than captured at build time, because the runtime
	// installs the hook after the flow (and so this continuation) exists.
	owner *Flow
	tail  *Flow
}

// Resume schedules msg through the rest of the flow. It hands off to the
// runtime, which owns both the workers to borrow and the flow-event publishing
// that makes the resumed tail a first-class invocation.
func (c *flowContinuation) Resume(ctx context.Context, msg *types.Message) error {
	resume := c.owner.resume
	if resume == nil {
		return errNoResume
	}
	return resume(ctx, msg, c.tail)
}
