package core

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/eip-go/types"
)

// forkRegistry extends the shared test registry with a "mutate" leaf that writes
// to the message it receives, so tests can observe per-branch clone isolation.
func forkRegistry() *BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("mutate", func(types.Settings, BlockDeps) (MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Variables.Set("touched", true)
			return msg, nil
		}), nil
	})
	return reg
}

func buildTestFork(t *testing.T, reg *BlockRegistry, p *pool, branches ...types.FlowConfig) *fork {
	t.Helper()
	proc, err := (&builder{reg: reg, pool: p}).fork(types.BlockConfig{Type: "fork", Branches: branches})
	if err != nil {
		t.Fatalf("buildFork: %v", err)
	}
	f, ok := proc.(*fork)
	if !ok {
		t.Fatalf("buildFork returned %T, want *fork", proc)
	}
	return f
}

func TestForkAllBranchesSucceed(t *testing.T) {
	reg := forkRegistry()
	p := newPool(4, 64)
	p.start()
	defer p.stop()

	f := buildTestFork(t, reg, p,
		types.FlowConfig{Name: "notify", Process: []types.BlockConfig{{Type: "mutate"}}},
		types.FlowConfig{Name: "audit", Process: []types.BlockConfig{{Type: "pass"}}},
	)

	msg := mustMessage(t)
	out, err := f.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("fork Process: %v", err)
	}
	if out != msg {
		t.Errorf("fork returned %p, want the input message %p", out, msg)
	}
	if _, ok := msg.Variables.Bool("touched"); ok {
		t.Error("a branch mutated the input message; clones are not isolated")
	}
}

func TestForkBranchErrorAborts(t *testing.T) {
	reg := forkRegistry()
	p := newPool(4, 64)
	p.start()
	defer p.stop()

	f := buildTestFork(t, reg, p,
		types.FlowConfig{Name: "ok", Process: []types.BlockConfig{{Type: "pass"}}},
		types.FlowConfig{Name: "bad", Process: []types.BlockConfig{{Type: "fail"}}},
	)

	out, err := f.Process(context.Background(), mustMessage(t))
	if err == nil {
		t.Fatal("fork with a failing branch returned nil error")
	}
	if out != nil {
		t.Errorf("fork returned %v on error, want nil", out)
	}
	if !strings.Contains(err.Error(), `fork branch "bad"`) {
		t.Errorf("error %q does not name the failing branch", err)
	}
}

func TestForkConcurrentBranchesAreIsolated(t *testing.T) {
	reg := forkRegistry()
	p := newPool(8, 128)
	p.start()
	defer p.stop()

	const branches = 50
	cfgs := make([]types.FlowConfig, branches)
	for i := range cfgs {
		cfgs[i] = types.FlowConfig{Process: []types.BlockConfig{{Type: "mutate"}}}
	}
	f := buildTestFork(t, reg, p, cfgs...)

	msg := mustMessage(t)
	if _, err := f.Process(context.Background(), msg); err != nil {
		t.Fatalf("fork Process: %v", err)
	}
	// With many branches each mutating their own clone, the run must be race-free
	// (go test -race) and leave the input message untouched.
	if _, ok := msg.Variables.Bool("touched"); ok {
		t.Error("concurrent branches mutated the shared input message")
	}
}
