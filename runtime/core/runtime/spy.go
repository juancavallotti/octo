package runtime

import (
	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// spyBlockType is the block type the injector wraps a target in. It matches the
// engine's internal spy kind, which the block builder recognizes.
const spyBlockType = "spy"

// injectSpies wraps each addressed block in an implicit spy block, so the engine
// records what crosses it. Unlike a breakpoint there can be many, and the flow runs
// to completion regardless.
//
// It mutates cfg in place — see resolveTarget for why that is safe.
func injectSpies(cfg *types.Config, spies *core.Spies) error {
	if spies == nil {
		return nil
	}
	for _, addr := range spies.Addresses() {
		target, err := resolveTarget(cfg, spyBlockType, addr)
		if err != nil {
			return err
		}
		*target = wrapInSpy(*target, addr)
	}
	return nil
}

// wrapInSpy returns the spy block that wraps target. The wrapper takes the target's
// label so a block error is reported against the same name it would have been
// without the spy, and carries the address so the engine knows which spy to file its
// records under.
func wrapInSpy(target types.BlockConfig, addr string) types.BlockConfig {
	return types.BlockConfig{
		Type:     spyBlockType,
		Name:     blockLabel(target),
		Settings: types.Settings{"address": addr},
		Process:  []types.BlockConfig{target},
	}
}
