package core

import (
	"context"
	"sync"
	"testing"

	"github.com/juancavallotti/eip-go/types"
)

// fakeSource is an inert MessageSource; tests feed the channel directly.
type fakeSource struct{}

func (fakeSource) Start(context.Context) error { return nil }
func (fakeSource) Stop(context.Context) error  { return nil }

// recorder collects published events in order.
type recorder struct {
	mu     sync.Mutex
	events []types.FlowEvent
}

func (r *recorder) handle(event types.FlowEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recorder) kinds() []types.FlowEventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	kinds := make([]types.FlowEventKind, len(r.events))
	for i, e := range r.events {
		kinds[i] = e.Kind
	}
	return kinds
}

func newTestFlow(t *testing.T, blockType string, workers int, rec *recorder) *boundFlow {
	t.Helper()
	bus := NewEventBus()
	bus.Subscribe(rec.handle)
	p := newPool(0, 0)
	root, err := buildFlow(types.FlowConfig{Process: []types.BlockConfig{{Type: blockType}}}, testRegistry(), p)
	if err != nil {
		t.Fatalf("buildFlow: %v", err)
	}
	return &boundFlow{
		name:    "test",
		source:  fakeSource{},
		root:    root,
		workers: workers,
		in:      make(chan *types.Message, 8),
		bus:     bus,
		pool:    p,
	}
}

func countKind(kinds []types.FlowEventKind, want types.FlowEventKind) int {
	n := 0
	for _, k := range kinds {
		if k == want {
			n++
		}
	}
	return n
}

func TestBoundFlowOutcomes(t *testing.T) {
	tests := []struct {
		name      string
		blockType string
		terminal  types.FlowEventKind
	}{
		{name: "completed", blockType: "pass", terminal: types.FlowEventCompleted},
		{name: "dropped", blockType: "drop", terminal: types.FlowEventDropped},
		{name: "failed", blockType: "fail", terminal: types.FlowEventFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			bf := newTestFlow(t, tt.blockType, 1, rec)
			if err := bf.start(context.Background()); err != nil {
				t.Fatalf("start: %v", err)
			}

			const n = 5
			for i := 0; i < n; i++ {
				bf.in <- mustMessage(t)
			}
			if err := bf.stop(context.Background()); err != nil {
				t.Fatalf("stop: %v", err)
			}

			kinds := rec.kinds()
			if got := countKind(kinds, types.FlowEventStarted); got != n {
				t.Errorf("started events = %d, want %d", got, n)
			}
			if got := countKind(kinds, tt.terminal); got != n {
				t.Errorf("%s events = %d, want %d", tt.terminal, got, n)
			}
		})
	}
}

func TestBoundFlowDrainsBeforeStopReturns(t *testing.T) {
	rec := &recorder{}
	bf := newTestFlow(t, "pass", 1, rec)
	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	const n = 20
	for i := 0; i < n; i++ {
		bf.in <- mustMessage(t)
	}
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// stop returned, so all in-flight messages must already be completed.
	if got := countKind(rec.kinds(), types.FlowEventCompleted); got != n {
		t.Errorf("completed events = %d, want %d", got, n)
	}
}
