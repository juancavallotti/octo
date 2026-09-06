// Package controlflow holds the blocks that shape how a flow runs rather than
// what it does to a message: branching (if, switch), iteration (foreach),
// scatter (fork), error boundaries (handle-errors), scopes (enrich, cache-scope),
// filtering (validate), and the two blocks that take the flow asynchronous
// (split, aggregate).
//
// Every block here is an ordinary registered block: it decodes its own settings
// struct — sub-flow slots included — and builds the nested chains it declares
// through core.BlockDeps.SubFlows. Nothing in this package reaches into the
// engine, which is the point: a block outside the runtime tree can do exactly
// what these do.
package controlflow

import (
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
)

// The block types this package registers.
const (
	blockKindHandleErrors    = "handle-errors"
	blockKindFork            = "fork"
	blockKindIf              = "if"
	blockKindSwitch          = "switch"
	blockKindForeach         = "foreach"
	blockKindEnrich          = "enrich"
	blockKindValidate        = "validate"
	blockKindCacheScope      = "cache-scope"
	blockKindInvalidateCache = "invalidate-cache"
	blockKindSplit           = "split"
	blockKindAggregate       = "aggregate"
)

// Palette groups the blocks fall under in the editor sidebar.
const (
	groupFlowControl  = "Flow Control"
	groupStorageCache = "Storage & Cache"
	// iconSplit is shared by the split block and the fork-shaped composites that
	// draw the same way.
	iconSplit = "Split"
)

// init is this module's manifest: the one place that says what the package puts
// into the block registry and the editor schema, in a deterministic order.
func init() {
	registerBranching()
	registerScopes()
	registerTakeover()
}

// registerBranching registers the blocks that choose or repeat a chain.
func registerBranching() {
	core.MustRegisterBlock(blockKindHandleErrors, newHandleErrors)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        blockKindHandleErrors,
		Label:       "Handle Errors",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        "ShieldAlert",
		Description: "Run the process chain; on error, expose vars.error and run the error chain (recovery).",
		Config:      reflect.TypeFor[handleErrorsSettings](),
	})
	core.MustRegisterBlock(blockKindFork, newFork)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        blockKindFork,
		Label:       "Fork",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        "GitFork",
		Description: "Scatter the message across parallel branches, then join and pass through.",
		Config:      reflect.TypeFor[forkSettings](),
	})
	core.MustRegisterBlock(blockKindIf, newIf)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        blockKindIf,
		Label:       "If",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        iconSplit,
		Description: "Conditional branching on a CEL boolean expression.",
		Config:      reflect.TypeFor[ifSettings](),
	})
	core.MustRegisterBlock(blockKindSwitch, newSwitch)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        blockKindSwitch,
		Label:       "Switch",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        iconSplit,
		Description: "Multi-case routing; runs the first matching case or the default.",
		Config:      reflect.TypeFor[switchSettings](),
	})
	core.MustRegisterBlock(blockKindForeach, newForeach)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindForeach,
		Label:    "For Each",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     "Repeat",
		Description: "Sequentially iterate over an array, running the body per element. In map mode, " +
			"each element's resulting body is collected into an array that replaces the message body.",
		Config: reflect.TypeFor[foreachSettings](),
	})
}

// registerScopes registers the blocks that run a chain on a scope of the message
// and fold something back: enrichment, validation, and caching.
func registerScopes() {
	core.MustRegisterBlock(blockKindEnrich, newEnrich)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindEnrich,
		Label:    "Enrich",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     "Sparkles",
		Description: "Run a body flow on an isolated copy of the message, then enrich the message " +
			"from the result: a CEL expression for the body and a CEL expression per variable.",
		Config: reflect.TypeFor[enrichSettings](),
	})
	core.MustRegisterBlock(blockKindValidate, newValidate)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindValidate,
		Label:    "Validate",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     "ShieldCheck",
		Description: "Filter block: assert a list of CEL rules against the message. If all hold, the " +
			"message passes through; if any fail, the block rejects — running the onReject sub-flow " +
			"(or a built-in response) and stopping the flow so the rest of the chain never runs. " +
			"Failing rule messages are exposed as vars.validationErrors.",
		Config: reflect.TypeFor[validateSettings](),
	})
	core.MustRegisterBlock(blockKindCacheScope, newCacheScope)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        blockKindCacheScope,
		Label:       "Cache",
		Category:    core.CategoryControlFlow,
		Group:       groupStorageCache,
		Icon:        "Clock",
		Description: "Memoize the body flow's result in the runtime store, keyed by a CEL expression.",
		Config:      reflect.TypeFor[cacheScopeSettings](),
	})
	core.MustRegisterBlock(blockKindInvalidateCache, newInvalidateCache)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        blockKindInvalidateCache,
		Label:       "Invalidate Cache",
		Category:    core.CategoryProcessor,
		Group:       groupStorageCache,
		Icon:        "Eraser",
		Description: "Evict a cache-scope entry by its key so the next run recomputes.",
		Config:      reflect.TypeFor[invalidateCacheSettings](),
	})
}

// registerTakeover registers the two blocks that take the flow asynchronous —
// the canonical EIP splitter and aggregator.
func registerTakeover() {
	core.MustRegisterBlock(blockKindSplit, newSplit)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindSplit,
		Label:    "Split",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     iconSplit,
		Description: "Split one message into many. Each element continues through the rest of the flow " +
			"as its own invocation, so elements are processed concurrently and one failing element " +
			"does not affect the others. The flow becomes asynchronous here: the buildResponse slot " +
			"shapes what the caller gets back.",
		Config: reflect.TypeFor[splitSettings](),
	})
	core.MustRegisterBlock(blockKindAggregate, newAggregate)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockKindAggregate,
		Label:    "Aggregate",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     "Merge",
		Description: "Combine many messages into one. Messages are grouped by a CEL expression and held " +
			"until the group completes — by size, by timeout, or by a predicate — then the group " +
			"continues through the rest of the flow as a single message. Pairs with split with no " +
			"configuration.",
		Config: reflect.TypeFor[aggregateSettings](),
	})
}
