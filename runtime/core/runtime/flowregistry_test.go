package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

const callTimeout = 2 * time.Second

// echoTerminal reads one message from ch and publishes a terminal event of the
// given kind keyed by that message's EventID, so a parked Call resolves.
func echoTerminal(bus *core.EventBus, ch <-chan *types.Message, kind types.FlowEventKind, body any, callErr error) {
	go func() {
		msg := <-ch
		result := msg.Clone()
		result.Body = body
		bus.Publish(types.FlowEvent{Kind: kind, EventID: msg.EventID, Result: result, Err: callErr})
	}()
}

func TestFlowRegistryCallCompleted(t *testing.T) {
	bus := core.NewEventBus()
	reg := newFlowRegistry(bus)
	ch := make(chan *types.Message, 1)
	if err := reg.register("target", ch); err != nil {
		t.Fatalf("register: %v", err)
	}
	echoTerminal(bus, ch, types.FlowEventCompleted, map[string]any{"ok": true}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := reg.Call(ctx, "target", mustMessage(t))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	body, _ := out.Body.(map[string]any)
	if body["ok"] != true {
		t.Errorf("result body = %v, want ok=true", out.Body)
	}
}

func TestFlowRegistryCallDropped(t *testing.T) {
	bus := core.NewEventBus()
	reg := newFlowRegistry(bus)
	ch := make(chan *types.Message, 1)
	_ = reg.register("target", ch)
	echoTerminal(bus, ch, types.FlowEventDropped, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	out, err := reg.Call(ctx, "target", mustMessage(t))
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out != nil {
		t.Errorf("dropped Call result = %v, want nil", out)
	}
}

func TestFlowRegistryCallFailed(t *testing.T) {
	bus := core.NewEventBus()
	reg := newFlowRegistry(bus)
	ch := make(chan *types.Message, 1)
	_ = reg.register("target", ch)
	wantErr := errors.New("boom")
	echoTerminal(bus, ch, types.FlowEventFailed, nil, wantErr)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	_, err := reg.Call(ctx, "target", mustMessage(t))
	if !errors.Is(err, wantErr) {
		t.Fatalf("Call err = %v, want %v", err, wantErr)
	}
}

func TestFlowRegistrySendOneWay(t *testing.T) {
	bus := core.NewEventBus()
	reg := newFlowRegistry(bus)
	ch := make(chan *types.Message, 1)
	_ = reg.register("target", ch)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	msg := mustMessage(t)
	if err := reg.Send(ctx, "target", msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case got := <-ch:
		if got.EventID != msg.EventID {
			t.Errorf("delivered EventID = %q, want %q", got.EventID, msg.EventID)
		}
	case <-time.After(callTimeout):
		t.Fatal("message was not delivered")
	}
}

func TestFlowRegistryUnknownFlow(t *testing.T) {
	bus := core.NewEventBus()
	reg := newFlowRegistry(bus)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := reg.Send(ctx, "missing", mustMessage(t)); err == nil {
		t.Fatal("Send to unknown flow: want error, got nil")
	}
	if _, err := reg.Call(ctx, "missing", mustMessage(t)); err == nil {
		t.Fatal("Call to unknown flow: want error, got nil")
	}
}

func TestFlowRegistryDuplicateRegistration(t *testing.T) {
	reg := newFlowRegistry(core.NewEventBus())
	ch := make(chan *types.Message, 1)
	if err := reg.register("dup", ch); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := reg.register("dup", ch); err == nil {
		t.Fatal("second register: want duplicate error, got nil")
	}
}
