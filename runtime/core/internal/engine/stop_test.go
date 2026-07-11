package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

// forkRaceBranches is how many concurrently-stopping branches
// TestStopFromEveryForkBranch scatters, enough to surface an unsynchronized write
// to the parent message under -race.
const forkRaceBranches = 8

// stopRegistry returns a registry with a "stop" block (requests stop and passes
// the message through), a "pass" block (a no-op), and a "record" block that flips
// the given flag when it runs, so tests can assert a stop short-circuits the rest
// of the chain.
func stopRegistry(ran *bool) *core.BlockRegistry {
	reg := core.NewBlockRegistry()
	reg.MustRegister("stop", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.RequestStop()
			return msg, nil
		}), nil
	})
	reg.MustRegister("pass", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			return msg, nil
		}), nil
	})
	reg.MustRegister("record", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			*ran = true
			return msg, nil
		}), nil
	})
	return reg
}

func TestStopShortCircuitsChain(t *testing.T) {
	var ran bool
	reg := stopRegistry(&ran)

	flow, err := (&builder{reg: reg, pool: pool.New(0, 0)}).flow(types.FlowConfig{
		Process: []types.BlockConfig{{Type: "stop"}, {Type: "record"}},
	})
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil {
		t.Fatal("stop must complete the flow, not drop it (out is nil)")
	}
	if !out.StopRequested() {
		t.Error("terminal message should carry the stop flag")
	}
	if ran {
		t.Error("block after stop must not run")
	}
}

func TestStopBubblesThroughComposite(t *testing.T) {
	var ran bool
	reg := stopRegistry(&ran)

	// if(true) -> then{ stop }, followed by a record block in the outer flow.
	flow, err := (&builder{reg: reg, pool: pool.New(0, 0)}).flow(types.FlowConfig{
		Process: []types.BlockConfig{
			{
				Type:      "if",
				Condition: "true",
				Then:      &types.FlowConfig{Process: []types.BlockConfig{{Type: "stop"}}},
			},
			{Type: "record"},
		},
	})
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("stop inside a composite must complete and bubble the flag to the root")
	}
	if ran {
		t.Error("block after a stop-bubbling composite must not run")
	}
}

func TestStopBubblesThroughFork(t *testing.T) {
	var ran bool
	reg := stopRegistry(&ran)

	// A fork schedules its branches on the pool, so the pool must be running: an
	// unstarted pool queues the branch tasks and the fork's join waits forever.
	p := pool.New(4, 64)
	p.Start()
	defer p.Stop()

	// fork{ branch "audit" -> stop; branch "notify" -> pass }, followed by a
	// record block in the outer flow. The branches run on clones, so the fork has
	// to carry the flag back to the parent message itself.
	flow, err := (&builder{reg: reg, pool: p}).flow(types.FlowConfig{
		Process: []types.BlockConfig{
			{
				Type: "fork",
				Branches: []types.FlowConfig{
					{Name: "audit", Process: []types.BlockConfig{{Type: "stop"}}},
					{Name: "notify", Process: []types.BlockConfig{{Type: "pass"}}},
				},
			},
			{Type: "record"},
		},
	})
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("a stop inside a fork branch must bubble out to the parent message")
	}
	if ran {
		t.Error("block after a stopping fork must not run")
	}
}

// TestStopFromEveryForkBranch pins the concurrency contract of the fork's stop
// propagation: the flag is collected under the fork's mutex and written to the
// parent message once, after the join. Writing it from the branch goroutines
// would race on the message's Variables map — under -race this test catches that.
func TestStopFromEveryForkBranch(t *testing.T) {
	var ran bool
	reg := stopRegistry(&ran)

	// Size the pool for the fan-out: Submit panics on a full queue rather than
	// deadlock, and every branch must be able to run concurrently for the race
	// detector to see the contended write.
	p := pool.New(forkRaceBranches, forkRaceBranches)
	p.Start()
	defer p.Stop()

	branches := make([]types.FlowConfig, 0, forkRaceBranches)
	for i := range forkRaceBranches {
		branches = append(branches, types.FlowConfig{
			Name:    fmt.Sprintf("branch-%d", i),
			Process: []types.BlockConfig{{Type: "stop"}},
		})
	}

	flow, err := (&builder{reg: reg, pool: p}).flow(types.FlowConfig{
		Process: []types.BlockConfig{{Type: "fork", Branches: branches}, {Type: "record"}},
	})
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("a fork whose branches all stop must bubble the flag to the parent message")
	}
	if ran {
		t.Error("block after a stopping fork must not run")
	}
}

func TestStopHaltsForeach(t *testing.T) {
	reg := core.NewBlockRegistry()
	var iterations int
	// A body that stops on the second element, counting how many times it ran.
	reg.MustRegister("count-then-stop", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			iterations++
			if iterations >= 2 {
				msg.RequestStop()
			}
			return msg, nil
		}), nil
	})

	flow, err := (&builder{reg: reg, pool: pool.New(0, 0)}).flow(types.FlowConfig{
		Process: []types.BlockConfig{{
			Type:  "foreach",
			Items: "[1, 2, 3, 4]",
			Body:  &types.FlowConfig{Process: []types.BlockConfig{{Type: "count-then-stop"}}},
		}},
	})
	if err != nil {
		t.Fatalf("build flow: %v", err)
	}

	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("foreach must complete and propagate the stop flag")
	}
	if iterations != 2 {
		t.Errorf("foreach ran %d iterations, want 2 (stopped early)", iterations)
	}
}
