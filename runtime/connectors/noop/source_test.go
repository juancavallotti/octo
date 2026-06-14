package noop

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func TestConnectorStartStop(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestSourceEmitsCount(t *testing.T) {
	const count = 4
	out := make(chan *types.Message, count)

	src, err := (&Connector{}).NewSource(
		types.SourceConfig{Type: "noop", Settings: map[string]any{"count": count}},
		out,
	)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	if err := src.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < count; i++ {
		select {
		case <-out:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for message %d", i)
		}
	}
	if err := src.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestServiceRunsNoopFlow drives the full Service lifecycle: a noop connector
// feeds a single flow, and the runtime completes every emitted message.
func TestServiceRunsNoopFlow(t *testing.T) {
	const count = 5

	var once sync.Once
	completed := 0
	var mu sync.Mutex
	done := make(chan struct{})
	core.DefaultEventBus().Subscribe(func(event types.FlowEvent) {
		if event.Flow != "noop-flow" || event.Kind != types.FlowEventCompleted {
			return
		}
		mu.Lock()
		completed++
		if completed == count {
			once.Do(func() { close(done) })
		}
		mu.Unlock()
	})

	config := types.Config{
		Connectors: []types.ConnectorConfig{{Name: "noop1", Type: "noop"}},
		Flows: []types.FlowConfig{{
			Name: "noop-flow",
			Source: &types.SourceConfig{
				Connector: "noop1",
				Type:      "noop",
				Settings:  map[string]any{"count": count},
			},
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- core.NewService(config, nil).Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("timed out waiting for completed events")
	}

	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
}
