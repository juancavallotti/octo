// Shared machinery for the blocks that take the flow asynchronous — split and
// aggregate. Both hand their work to a continuation and then have the same
// problem: the caller is still waiting, and the real answer no longer exists at
// the point the caller is waiting at. The buildResponse slot is where a flow
// decides what to say instead.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// hashedKey reduces an arbitrary string to a bounded, path-safe KV key. Both the
// user-supplied halves of a stored key — a cache key expression's result, an
// aggregate group's correlation value — go through it, because a KV backend may
// put the key in a URL path (the k8s module's API does).
func hashedKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// The variables that correlate messages belonging to one group. A "split" block
// writes all three onto every element; an "aggregate" block reads them by
// default and can be pointed at anything else, since every place it reads them
// is a CEL expression.
//
// They are named for the group rather than the split because a group is the more
// general idea: an aggregator batching webhook events that never passed through
// a splitter deserves the same vocabulary.
const (
	// varGroupID identifies the group. A split sets it to the EventID of the
	// message being split, which every element agrees on because they were all
	// derived from it — and which survives the rekey each element gets.
	varGroupID = "groupId"
	// varGroupIndex is an element's 0-based position within its group.
	varGroupIndex = "groupIndex"
	// varGroupSize is how many messages the group holds, or -1 while that is not
	// yet knowable. Every element carries it when the total is known up front; a
	// streaming split can only stamp it on the final element. Either way the group
	// learns its size exactly once, which is what lets an aggregator complete on a
	// count rather than on a last-marker — the only order-independent option, since
	// elements run concurrently and routinely finish out of order.
	varGroupSize = "groupSize"
)

// buildTakeoverResponse compiles a takeover block's optional buildResponse
// sub-flow. It returns nil when the slot is absent or carries no steps: the
// editor seeds an empty sub-flow into every composite slot, so an empty one must
// read as "unset" rather than as a flow that blanks the response.
func buildTakeoverResponse(b *builder, cfg *types.FlowConfig) (*Flow, error) {
	if cfg == nil || len(cfg.Process) == 0 {
		return nil, nil
	}
	flow, err := b.branch(core.BranchBuildResponse).subFlow(*cfg)
	if err != nil {
		return nil, fmt.Errorf("buildResponse: %w", err)
	}
	return flow, nil
}

// applyTakeover ends the synchronous half of the flow. The block has already
// handed its work to a continuation, so there is nothing left to carry down the
// chain — only an answer to give whoever is still waiting.
//
// With a buildResponse sub-flow that flow shapes the answer; a sub-flow that
// drops the message drops it here too. Without one the message stops as it
// stands, which completes the flow with the body it already had — a reasonable
// "accepted" for a caller that did not ask for more.
//
// Either way the survivor is marked via RequestStop, so the engine completes the
// flow with it rather than running the blocks that follow. Those blocks are the
// continuation's, and running them here as well would process every message
// twice.
func applyTakeover(ctx context.Context, msg *types.Message, buildResponse *Flow) (*types.Message, error) {
	if buildResponse != nil {
		out, err := buildResponse.Process(ctx, msg)
		if err != nil {
			return nil, err
		}
		if out == nil {
			return nil, nil
		}
		msg = out
	}
	msg.RequestStop()
	return msg, nil
}

// requireRootChain rejects a takeover block that is not in a flow's own process
// or error chain. A composite's sub-flow ends at the composite, which then
// post-processes the result, so there is no "rest of the flow" for the block to
// continue into — and a build error is the only place to say so where the author
// is still looking.
func requireRootChain(b *builder, kind string) error {
	if b.root {
		return nil
	}
	return fmt.Errorf(
		"%s block must be a top-level block in a flow's chain, not inside another block's slot", kind)
}
