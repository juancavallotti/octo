package ai

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/engine"
	"github.com/juancavallotti/octo/runtime/internal/testkit"
	"github.com/juancavallotti/octo/runtime/types"
)

// The shared fixtures, under the names this package's tests grew up with.
type (
	fakeKV        = testkit.FakeKV
	fakeMemory    = testkit.FakeMemory
	fakeServices  = testkit.FakeServices
	processorFunc = testkit.ProcessorFunc
)

var (
	newFakeKV        = testkit.NewFakeKV
	withFakeServices = testkit.WithFakeServices
	withFakeMemory   = testkit.WithFakeMemory
	testRegistry     = testkit.Registry
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

// recordRegistry extends the shared test registry with a "record" leaf that
// appends a marker to a shared slice, so tests can observe which sub-flows ran
// and in what order. A block records its "tag" setting verbatim, or the message
// variable named by its "var" setting.
func recordRegistry(seen *[]any) *core.BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("record", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		tag, hasTag := s.String("tag")
		varName, _ := s.String("var")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			if hasTag {
				*seen = append(*seen, tag)
			} else {
				*seen = append(*seen, msg.Variables[varName])
			}
			return msg, nil
		}), nil
	})
	return reg
}

// tagFlow is a one-block flow that records the given marker when it runs.
func tagFlow(tag string) types.FlowConfig {
	return types.FlowConfig{Process: []types.BlockConfig{
		{Type: "record", Settings: types.Settings{"tag": tag}},
	}}
}

// agentSettingsOf decodes a block config the way the ai-agent factory does, for
// a test that checks validation without building.
func agentSettingsOf(t testing.TB, cfg types.BlockConfig) agentSettings {
	t.Helper()
	var settings agentSettings
	if err := cfg.Settings.DecodeStrict(&settings); err != nil {
		t.Fatalf("decode agent settings: %v", err)
	}
	return settings
}
