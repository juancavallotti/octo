package runtime

import (
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// mockBlockType is the block type the injector puts in a target's place. It matches
// the engine's internal mock kind, which the block builder recognizes.
const mockBlockType = "mock"

// injectMocks replaces each addressed block with an implicit mock block, so the
// engine answers for it instead of running it. Unlike a breakpoint or a spy this
// removes the block — and, when the target is a composite, everything inside it.
//
// It mutates cfg in place — see resolveTarget for why that is safe.
func injectMocks(cfg *types.Config, mocks *core.Mocks) error {
	if mocks == nil {
		return nil
	}
	for _, addr := range mocks.Addresses() {
		err := rewriteTarget(cfg, mockBlockType, addr, func(target types.BlockConfig) types.BlockConfig {
			return standInMock(target, addr)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// standInMock returns the mock block that stands in for target. It takes the
// target's label, so a mocked block that fails reports as the block the user wrote,
// and carries the address so the engine can find the spec to answer with.
//
// Nothing of the target survives: a mock replaces the block, and mocking a composite
// is how you cut out a whole sub-tree that would otherwise call the network.
func standInMock(target types.BlockConfig, addr string) types.BlockConfig {
	return types.BlockConfig{
		Type:     mockBlockType,
		Name:     blockLabel(target),
		Settings: types.Settings{"address": addr},
	}
}

// checkMockConflicts rejects an address that points *inside* a block being mocked.
//
// A mock replaces its target, so everything the target contained is gone from the
// tree before a spy or a breakpoint is injected. Left alone, spying a block inside a
// mocked fork would fail with "no block X in that chain" — which reads like the block
// does not exist, when the truth is that the user mocked away the thing they were
// trying to watch. It is checked before any injection, while both addresses still
// resolve, so the message can name both.
//
// others are the addresses of the features that observe a block rather than replace
// it — the ones a mock can strand — each carrying the kind that asked for it, so the
// conflict is reported against the flag the user typed.
func checkMockConflicts(mocks *core.Mocks, others []resolver) error {
	if mocks == nil {
		return nil
	}
	for _, mocked := range mocks.Addresses() {
		outer, err := resolver{kind: mockBlockType, addr: mocked}.parseAddress()
		if err != nil {
			return err
		}
		for _, other := range others {
			inner, parseErr := other.parseAddress()
			if parseErr != nil {
				return parseErr
			}
			if isInside(outer, inner) {
				return fmt.Errorf(
					"%s %q is inside mocked block %q: the mock replaces that block, so there is nothing in there to %s",
					other.kind, other.addr, mocked, other.kind)
			}
		}
	}
	return nil
}

// isInside reports whether inner addresses a block within the block outer addresses:
// same flow and chain, and outer's path is a proper prefix of inner's.
func isInside(outer, inner address) bool {
	if outer.flow != inner.flow || outer.chain != inner.chain {
		return false
	}
	if len(inner.steps) <= len(outer.steps) {
		return false
	}

	for i, step := range outer.steps {
		if step.block != inner.steps[i].block {
			return false
		}
		// Every step but outer's last names the branch it descends into, and those
		// have to match too — the same block name reached through a different branch
		// is a different block. Outer's last step names no branch (it is the target),
		// while inner's carries the branch it descends into, so it is not compared.
		if i < len(outer.steps)-1 && step.branch != inner.steps[i].branch {
			return false
		}
	}
	return true
}
