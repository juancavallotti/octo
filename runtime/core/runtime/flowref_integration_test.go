package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// startInvokeService runs svc in invoke mode and waits until it is ready, returning
// a cancel/cleanup the caller defers.
func startInvokeService(t *testing.T, cfg types.Config) (*Service, func()) {
	t.Helper()
	svc := NewService(cfg, core.DefaultRegistry(), WithInvokeMode())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	select {
	case <-svc.Started():
	case err := <-done:
		t.Fatalf("service stopped before ready: %v", err)
	case <-time.After(e2eWaitTimeout):
		cancel()
		t.Fatal("service did not become ready")
	}
	return svc, func() {
		cancel()
		<-done
	}
}

// TestFlowRefTwoWayFoldsResult verifies a synchronous flow-ref folds the called
// flow's body and variables back into the caller's message.
func TestFlowRefTwoWayFoldsResult(t *testing.T) {
	core.MustRegisterBlock("tref.setresult", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Body = map[string]any{"ok": true}
			msg.Variables.Set("tag", "t")
			return msg, nil
		}), nil
	})

	cfg := types.Config{
		Flows: []types.FlowConfig{
			{Name: "caller", Process: []types.BlockConfig{
				{Type: "flow-ref", Settings: types.Settings{"flow": "target"}},
			}},
			{Name: "target", Process: []types.BlockConfig{{Type: "tref.setresult"}}},
		},
	}
	svc, stop := startInvokeService(t, cfg)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := svc.Flows().Call(ctx, "caller", mustMessage(t))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	body, _ := out.Body.(map[string]any)
	if body["ok"] != true {
		t.Errorf("folded body = %v, want ok=true", out.Body)
	}
	if tag, _ := out.Variables.String("tag"); tag != "t" {
		t.Errorf("folded variable tag = %q, want t", tag)
	}
}

// TestFlowRefOneWayFireAndForget verifies a one-way flow-ref runs the target for
// its side effect while leaving the caller's message unchanged.
func TestFlowRefOneWayFireAndForget(t *testing.T) {
	signal := make(chan struct{}, 1)
	core.MustRegisterBlock("tref.signal", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			select {
			case signal <- struct{}{}:
			default:
			}
			return msg, nil
		}), nil
	})

	cfg := types.Config{
		Flows: []types.FlowConfig{
			{Name: "caller-ow", Process: []types.BlockConfig{
				{Type: "flow-ref", Settings: types.Settings{"flow": "target-ow", "oneWay": true}},
			}},
			{Name: "target-ow", Process: []types.BlockConfig{{Type: "tref.signal"}}},
		},
	}
	svc, stop := startInvokeService(t, cfg)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	in := mustMessage(t)
	in.Body = map[string]any{"keep": "me"}
	out, err := svc.Flows().Call(ctx, "caller-ow", in)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if body, _ := out.Body.(map[string]any); body["keep"] != "me" {
		t.Errorf("one-way changed caller body: %v", out.Body)
	}
	select {
	case <-signal:
	case <-time.After(callTimeout):
		t.Fatal("one-way target did not run")
	}
}
