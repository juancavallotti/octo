package core

import (
	"context"

	"github.com/juancavallotti/octo/runtime/types"
)

// Continuation is the rest of a flow, handed to the block that sits in front of
// it. It is how a block takes over execution: rather than returning the next
// message and letting the engine walk on, the block ends its own chain and
// continues the flow itself — zero times, once, or many times, now or minutes
// from now, on workers borrowed from the flow's pool.
//
// That is what the two message-multiplicity patterns need and what a
// one-in/one-out processor cannot express. A splitter resumes once per element,
// so each element becomes its own invocation with its own EventID and its own
// error fate. An aggregator resumes once per completed group, long after the
// message that opened it returned.
//
// A continuation is bound to a position at build time, not to an invocation:
// every block that takes over gets its own, covering the blocks that follow it.
// So there is nothing to correlate at resume time, and — because every replica
// builds the same flow from the same config — a group completed on one replica
// resumes at the same place on any other.
type Continuation interface {
	// Resume schedules msg through the rest of the flow, which runs as an
	// invocation in its own right: it publishes its own flow events, keyed on
	// msg's EventID, and the flow's error chain applies to it.
	//
	// It returns once the work is queued, not once it finishes. It blocks only
	// for admission — a saturated pool parks the caller, and that is the
	// backpressure that keeps a large split from outrunning its consumers.
	Resume(ctx context.Context, msg *types.Message) error
}

// FlowResume schedules msg through tail and reports the outcome. It is the half
// of a continuation the engine cannot supply: the worker pool to borrow from and
// the flow-event publishing both belong to the runtime's bound flow, while only
// the engine's Flow knows what "the rest of the chain" is. The runtime installs
// one on each root flow at build time.
type FlowResume func(ctx context.Context, msg *types.Message, tail MessageProcessor) error
