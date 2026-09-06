// Package testkit holds the fixtures the runtime's block and engine tests share:
// in-memory runtime services, an in-memory agent memory store, a registry of
// trivial leaf blocks, and the small adapters a test needs to build a flow
// outside the runtime.
//
// It imports core only. The engine's own tests use it too, so it cannot import
// the engine; a test that needs to build a flow calls engine.BuildRoot with the
// Scheduler and Registry from here.
package testkit

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// ProcessorFunc adapts a function to the core.MessageProcessor interface.
type ProcessorFunc func(ctx context.Context, msg *types.Message) (*types.Message, error)

// Process calls the function.
func (f ProcessorFunc) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	return f(ctx, msg)
}

// Inherit returns a fresh registry holding every block the test binary
// registered on the default registry, and nothing else — for a test that wants
// the real composites beside leaves of its own naming.
func Inherit() *core.BlockRegistry {
	reg := core.NewBlockRegistry()
	defaults := core.DefaultBlockRegistry()
	for _, name := range defaults.Names() {
		if factory, ok := defaults.Factory(name); ok {
			reg.MustRegister(name, factory)
		}
	}
	return reg
}

// Registry returns a registry holding every block the test binary registered on
// the default registry — the composites and leaves the packages under test put
// there — plus the leaf blocks flow tests lean on: "pass" returns the message,
// "drop" drops it, "fail" errors.
func Registry() *core.BlockRegistry {
	reg := Inherit()
	reg.MustRegister("pass", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return ProcessorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			return msg, nil
		}), nil
	})
	reg.MustRegister("drop", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return ProcessorFunc(func(context.Context, *types.Message) (*types.Message, error) {
			return nil, nil //nolint:nilnil // dropping the message is the block's contract
		}), nil
	})
	reg.MustRegister("fail", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return ProcessorFunc(func(context.Context, *types.Message) (*types.Message, error) {
			return nil, errors.New("boom")
		}), nil
	})
	return reg
}

// Message returns a fresh message, failing the test if one cannot be minted.
func Message(t testing.TB) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	return msg
}

// Scheduler runs each submitted task on its own goroutine, which is what a real
// pool does from a block's point of view.
func Scheduler() core.SchedulerFunc {
	return func(task func()) { go task() }
}
