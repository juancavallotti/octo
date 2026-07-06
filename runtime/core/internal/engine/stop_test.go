package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

// stopRegistry returns a registry with a "stop" block (requests stop and passes
// the message through) and a "record" block that flips the given flag when it
// runs, so tests can assert a stop short-circuits the rest of the chain.
func stopRegistry(ran *bool) *core.BlockRegistry {
	reg := core.NewBlockRegistry()
	reg.MustRegister("stop", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.RequestStop()
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
