package core_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeSink records what the drain goroutine wrote, and can be told to block so a
// test can fill the queue deterministically.
type fakeSink struct {
	mu      sync.Mutex
	written []types.TraceEvent
	flushes int
	closed  bool

	// gate, when non-nil, holds every Write until it is closed. It is what lets
	// a test park the drain goroutine and overflow the queue behind it.
	gate chan struct{}
	// writeErr is returned by every Write when set.
	writeErr error
}

func (f *fakeSink) Write(event types.TraceEvent) error {
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written = append(f.written, event)
	return nil
}

func (f *fakeSink) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushes++
	return nil
}

func (f *fakeSink) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeSink) records() []types.TraceEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]types.TraceEvent(nil), f.written...)
}

func TestBufferedTracerDrainsAndCloses(t *testing.T) {
	sink := &fakeSink{}
	tracer := core.NewBufferedTracer(sink, core.TraceOptions{Enabled: true, Buffer: 16})

	if !tracer.Enabled() {
		t.Fatal("a fresh tracer reports disabled")
	}
	for range 5 {
		tracer.Publish(types.TraceEvent{Kind: types.TraceFlowStarted, Flow: "orders"})
	}
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Close drains what was queued before it closed the sink: a record published
	// a microsecond before shutdown is exactly the one worth keeping.
	if got := len(sink.records()); got != 5 {
		t.Fatalf("written = %d, want 5", got)
	}
	if !sink.closed {
		t.Fatal("Close did not close the sink")
	}
	if tracer.Enabled() {
		t.Fatal("a closed tracer still reports enabled")
	}
	// Publishing after Close is a no-op, not a panic on a closed channel.
	tracer.Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
	if got := len(sink.records()); got != 5 {
		t.Fatalf("written = %d after publishing post-Close, want 5", got)
	}
	if err := tracer.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestBufferedTracerStampsSequence pins that Seq is the publisher's to assign:
// whatever a caller put there is overwritten, so the numbering is a property of
// the stream rather than of whoever happened to emit into it.
func TestBufferedTracerStampsSequence(t *testing.T) {
	sink := &fakeSink{}
	tracer := core.NewBufferedTracer(sink, core.TraceOptions{Enabled: true, Buffer: 16})

	for range 3 {
		tracer.Publish(types.TraceEvent{Kind: types.TraceFlowStarted, Seq: 99})
	}
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	records := sink.records()
	if len(records) != 3 {
		t.Fatalf("written = %d, want 3", len(records))
	}
	for i, record := range records {
		if want := uint64(i + 1); record.Seq != want {
			t.Fatalf("record %d Seq = %d, want %d", i, record.Seq, want)
		}
	}
}

// TestBufferedTracerTailDropsAndAnnounces is the loss contract. A full queue
// drops rather than blocking — a tracer must never put a flow worker behind a
// disk write — and the stream says so in-band, because a trace with an
// unannounced hole is worse than no trace: every conclusion drawn from the
// records that survived is quietly wrong.
func TestBufferedTracerTailDropsAndAnnounces(t *testing.T) {
	gate := make(chan struct{})
	sink := &fakeSink{gate: gate}
	tracer := core.NewBufferedTracer(sink, core.TraceOptions{Enabled: true, Buffer: 2})

	// The drain goroutine parks in Write, so the queue fills and stays full.
	// Publishing well past the buffer guarantees drops without depending on how
	// many records the drain managed to take first.
	for range 50 {
		tracer.Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
	}
	if tracer.Dropped() == 0 {
		t.Fatal("a full queue dropped nothing")
	}
	dropped := tracer.Dropped()

	// Let the drain run: the next record it writes must be preceded by the
	// marker naming the gap.
	close(gate)
	tracer.Publish(types.TraceEvent{Kind: types.TraceFlowCompleted})
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var marker *types.TraceEvent
	written := sink.records()
	for i := range written {
		if written[i].Kind == types.TraceDropped {
			marker = &written[i]
			break
		}
	}
	if marker == nil {
		t.Fatalf("no %s record after %d drops", types.TraceDropped, dropped)
	}
	if got, ok := marker.Attrs["total"].(uint64); !ok || got == 0 {
		t.Fatalf("marker total = %v, want a non-zero count", marker.Attrs["total"])
	}
	if tracer.Emitted() == 0 {
		t.Fatal("nothing was emitted")
	}
}

// TestBufferedTracerSurvivesSinkErrors: a sink that cannot write must not become
// a runtime that cannot run. The failures are counted rather than propagated,
// because there is nobody above the drain goroutine to hand them to.
func TestBufferedTracerSurvivesSinkErrors(t *testing.T) {
	sink := &fakeSink{writeErr: errors.New("disk full")}
	tracer := core.NewBufferedTracer(sink, core.TraceOptions{Enabled: true, Buffer: 8})

	for range 4 {
		tracer.Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
	}
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if tracer.Emitted() != 0 {
		t.Fatalf("emitted = %d, want 0 when every write failed", tracer.Emitted())
	}
}

// TestBufferedTracerConcurrentPublish runs the hot path the way it actually runs
// — every flow worker at once — so the race detector sees the sequence counter
// and the queue under contention.
func TestBufferedTracerConcurrentPublish(t *testing.T) {
	sink := &fakeSink{}
	tracer := core.NewBufferedTracer(sink, core.TraceOptions{Enabled: true, Buffer: 1024})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				tracer.Publish(types.TraceEvent{Kind: types.TraceBlockPostInvoke})
			}
		}()
	}
	wg.Wait()
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Seq must be unique — it is what a reader sorts and de-duplicates by. It is
	// deliberately NOT asserted to arrive in order: a publisher stamps the
	// number and then queues the record, so a goroutine preempted between the
	// two lets a later number reach the sink first. Sorting is the reader's job.
	seen := make(map[uint64]bool)
	for _, record := range sink.records() {
		if record.Kind == types.TraceDropped {
			continue
		}
		if seen[record.Seq] {
			t.Fatalf("Seq %d was handed out twice", record.Seq)
		}
		seen[record.Seq] = true
	}

	// The arithmetic that makes a trace auditable: everything published either
	// landed or was counted as lost.
	if total := tracer.Emitted() + tracer.Dropped(); total != 1600 {
		t.Fatalf("emitted+dropped = %d, want 1600", total)
	}
	if uint64(len(seen)) != tracer.Emitted() {
		t.Fatalf("distinct records = %d, emitted = %d", len(seen), tracer.Emitted())
	}
}
