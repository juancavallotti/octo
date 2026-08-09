package core_test

import (
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// withTracer installs a publisher for one test and restores whatever was there
// before. The tracer is process-wide, so a test that installs one without this
// leaks it into every test that runs after it.
func withTracer(t *testing.T, publisher core.TracePublisher) {
	t.Helper()
	previous := core.Tracer()
	core.SetTracer(publisher)
	t.Cleanup(func() { core.SetTracer(previous) })
}

// recordingTracer keeps what it is given, for tests that assert on emission
// rather than on delivery.
type recordingTracer struct {
	mu      sync.Mutex
	records []types.TraceEvent
}

func (r *recordingTracer) Enabled() bool { return true }

func (r *recordingTracer) Publish(event types.TraceEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, event)
}

func (r *recordingTracer) all() []types.TraceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]types.TraceEvent(nil), r.records...)
}

// TestTracerDefaultsToDisabledNoop is the property every emitter relies on: the
// lookup is safe to call before anything is installed, and it reports disabled,
// so a guarded caller costs an atomic load and builds nothing.
func TestTracerDefaultsToDisabledNoop(t *testing.T) {
	withTracer(t, nil)

	tracer := core.Tracer()
	if tracer == nil {
		t.Fatal("Tracer() returned nil")
	}
	if tracer.Enabled() {
		t.Fatal("the default tracer reports enabled")
	}
	// Publishing to it must be a no-op rather than a panic: an emitter that
	// forgets the Enabled check should waste work, not kill the process.
	tracer.Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
}

func TestSetTracerInstallsAndRestores(t *testing.T) {
	withTracer(t, nil)

	recorder := &recordingTracer{}
	core.SetTracer(recorder)
	if !core.Tracer().Enabled() {
		t.Fatal("installed tracer reports disabled")
	}
	core.Tracer().Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
	if got := len(recorder.all()); got != 1 {
		t.Fatalf("records = %d, want 1", got)
	}

	// nil re-installs the no-op rather than leaving a dangling publisher.
	core.SetTracer(nil)
	if core.Tracer().Enabled() {
		t.Fatal("SetTracer(nil) left an enabled tracer")
	}
	core.Tracer().Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
	if got := len(recorder.all()); got != 1 {
		t.Fatalf("records = %d after SetTracer(nil), want 1", got)
	}
}

// TestTracerConcurrentAccess is a race-detector test: the lookup runs on every
// flow worker while a reload could be installing a new publisher.
func TestTracerConcurrentAccess(t *testing.T) {
	withTracer(t, nil)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				if core.Tracer().Enabled() {
					core.Tracer().Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 500 {
			if i%2 == 0 {
				core.SetTracer(&recordingTracer{})
			} else {
				core.SetTracer(nil)
			}
		}
	}()
	wg.Wait()
}
