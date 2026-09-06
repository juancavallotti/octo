package cli

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
	processorFunc = testkit.ProcessorFunc
)

var (
	testRegistry = testkit.Registry
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

// mapResources is a core.ResourceLoader backed by an in-memory map keyed by id.
type mapResources map[string]string

func (m mapResources) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	body, ok := m[id]
	if !ok {
		return nil, core.ErrResourceNotFound
	}
	return []byte(body), nil
}

// depsRes builds block deps carrying only an in-memory resource loader.
func depsRes(res mapResources) core.BlockDeps {
	return core.BlockDeps{Resources: res}
}

// recordRegistry extends the shared test registry with a "record" leaf that
// appends a marker to a shared slice, so tests can observe which sub-flows ran.
func recordRegistry(seen *[]any) *core.BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("record", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		tag, _ := s.String("tag")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			*seen = append(*seen, tag)
			return msg, nil
		}), nil
	})
	return reg
}

func newMessageBody(t *testing.T, body string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := msg.SetBodyJSON([]byte(body)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return msg
}
