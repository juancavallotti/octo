package controlflow

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/engine"
	"github.com/juancavallotti/octo/runtime/internal/testkit"
	"github.com/juancavallotti/octo/runtime/types"
)

// The shared fixtures, under the names this package's tests grew up with.
type (
	fakeServices  = testkit.FakeServices
	processorFunc = testkit.ProcessorFunc
)

var (
	newFakeKV        = testkit.NewFakeKV
	withFakeServices = testkit.WithFakeServices
	testRegistry     = testkit.Registry
	inheritRegistry  = testkit.Inherit
	mustMessage      = testkit.Message
)

// buildBlock builds one block through the engine, the way the runtime would:
// as the sole block of a root chain, so a block that needs the sub-flow seam or
// a root chain gets both. It returns the block's processor.
//
//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func buildBlock(
	t testing.TB, reg *core.BlockRegistry, deps core.BlockDeps, cfg types.BlockConfig,
) core.MessageProcessor {
	t.Helper()
	proc, err := tryBuildBlock(reg, deps, cfg)
	if err != nil {
		t.Fatalf("build %s: %v", cfg.Type, err)
	}
	return proc
}

// tryBuildBlock is buildBlock for a test that expects the build to fail.
//
//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func tryBuildBlock(reg *core.BlockRegistry, deps core.BlockDeps, cfg types.BlockConfig) (core.MessageProcessor, error) {
	chain := []types.BlockConfig{cfg}
	// A block that takes the flow over needs something after it to continue
	// into, so the chain gets a trailing pass when the registry has one.
	if _, ok := reg.Factory("pass"); ok {
		chain = append(chain, types.BlockConfig{Type: "pass"})
	}
	flow, err := engine.BuildRoot(
		types.FlowConfig{Name: "test", Process: chain}, reg, testkit.Scheduler(), nil, deps)
	if err != nil {
		return nil, err
	}
	return flow.Blocks[0].Processor, nil
}
