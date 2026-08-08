package core

import "strconv"

// A block's path is its address in the runtime's grammar:
//
//	<flow>[<chain>].<block>[<branch>].<block>…
//
// e.g. `orders.checkHeader[else].api-call-1`. Every step but the last names a
// composite and the branch of it to descend into; the last step names the block
// itself. The flow's optional bracket selects its error chain
// (`orders[error].notify`).
//
// The same grammar is spoken by the address resolver behind `invoke --break-at`,
// `--spies` and `--mocks` (core/runtime/address.go), by the editor's
// naturalAddress (packages/editor/src/app/run/address.ts), and by the path the
// flow builder mints for each block so block events can report where they came
// from. The four helpers below are the one definition of how a path is spelled,
// so those derivations cannot drift apart character by character.

// The branch names every composite spells the same way. The first two are also a
// flow's own chains, selected by the bracket on the flow segment. A composite
// whose branches are a list — a fork's branches, a switch's cases, an ai-router's
// routes, an ai-agent's tools — is addressed by the member's own name instead,
// or by its index (see MemberBranch).
const (
	BranchProcess = "process"
	BranchError   = "error"
	BranchThen    = "then"
	BranchElse    = "else"
	BranchBody    = "body"
	BranchDefault = "default"
	// BranchOnReject and BranchBuildResponse are the two chains that shape what a
	// caller gets back rather than carrying the message onward: the first when a
	// filter rejects it, the second when a block takes the flow asynchronous and
	// the caller needs an answer before the real work has finished.
	BranchOnReject      = "onReject"
	BranchBuildResponse = "buildResponse"
	// BranchEvents is the observer chain an ai-agent runs once per agent event. It
	// neither carries the message onward nor answers the caller: its result is
	// discarded, so it is the one branch that cannot change the run it reports on.
	BranchEvents = "events"
)

// BlockLabel is how a block is named in an address: its name, else its type,
// else the processor it refs — so an unnamed block is still addressable, as long
// as it is the only one of its kind in its chain.
//
// This is deliberately not the label a block error carries (see engine's
// blockLabel), which falls back to the *effective* type after a ref is resolved.
// A block declared as `{ref: charge-api}` is addressed as `charge-api` but
// reports errors as the `rest` block it resolves to.
func BlockLabel(name, blockType, ref string) string {
	switch {
	case name != "":
		return name
	case blockType != "":
		return blockType
	default:
		return ref
	}
}

// MemberBranch is how one member of a list-valued branch is addressed — a fork's
// branch, a switch's case, an ai-router's route, an ai-agent's tool: by its own
// name, or by its index when it has none.
func MemberBranch(name string, index int) string {
	if name != "" {
		return name
	}
	return strconv.Itoa(index)
}

// JoinBlock extends a path with the block it selects out of the chain at prefix.
func JoinBlock(prefix, label string) string {
	return prefix + "." + label
}

// JoinBranch extends a block's own path with the branch of it to descend into.
func JoinBranch(blockPath, branch string) string {
	return blockPath + "[" + branch + "]"
}
