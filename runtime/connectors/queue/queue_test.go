package queue

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// startConnector starts a queue connector and registers cleanup.
func startConnector(t *testing.T) *Connector {
	t.Helper()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

func TestTrackReceivesMatchingFlowEvent(t *testing.T) {
	c := startConnector(t)
	ch := c.track("evt-1")
	defer c.forget("evt-1")

	want := &types.Message{EventID: "evt-1", Body: "done"}
	core.DefaultEventBus().Publish(types.FlowEvent{
		Kind: types.FlowEventCompleted, EventID: "evt-1", Result: want, OccurredAt: time.Now(),
	})

	select {
	case got := <-ch:
		if got.kind != types.FlowEventCompleted {
			t.Fatalf("kind = %q, want completed", got.kind)
		}
		if got.msg != want {
			t.Fatalf("msg = %+v, want %+v", got.msg, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tracked event")
	}
}

func TestTrackIgnoresStartedAndUnknownEvents(t *testing.T) {
	c := startConnector(t)
	ch := c.track("evt-2")
	defer c.forget("evt-2")

	// Started events carry no result; an event for another EventID is not ours.
	core.DefaultEventBus().Publish(types.FlowEvent{Kind: types.FlowEventStarted, EventID: "evt-2"})
	core.DefaultEventBus().Publish(types.FlowEvent{Kind: types.FlowEventCompleted, EventID: "other"})

	select {
	case got := <-ch:
		t.Fatalf("received unexpected result %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestForgetStopsDelivery(t *testing.T) {
	c := startConnector(t)
	ch := c.track("evt-3")
	c.forget("evt-3")

	core.DefaultEventBus().Publish(types.FlowEvent{
		Kind: types.FlowEventCompleted, EventID: "evt-3", Result: &types.Message{EventID: "evt-3"},
	})

	select {
	case got := <-ch:
		t.Fatalf("received result after forget: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}
