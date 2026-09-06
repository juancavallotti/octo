package runtime

import "github.com/juancavallotti/octo/runtime/types"

// breakpointBlockType is the block type the injector wraps a target in. It matches
// the engine's internal breakpoint kind, which the block builder recognizes.
const breakpointBlockType = "breakpoint"

// injectBreakpoint wraps the block named by addr in an implicit breakpoint block,
// so the engine records the message there and halts the flow.
//
// It mutates cfg in place — see resolveTarget for why that is safe. Call it exactly
// once per config.
func injectBreakpoint(cfg *types.Config, addr string) error {
	return rewriteTarget(cfg, breakpointBlockType, addr, wrapInBreakpoint)
}

// wrapInBreakpoint returns the breakpoint block that wraps target. The wrapper
// takes the target's label so a block error is reported against the same name it
// would have been without the breakpoint.
func wrapInBreakpoint(target types.BlockConfig) types.BlockConfig {
	return types.BlockConfig{
		Type:     breakpointBlockType,
		Name:     blockLabel(target),
		Settings: types.Settings{"process": []types.BlockConfig{target}},
	}
}
