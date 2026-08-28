package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// telemetryBackend collects what the runtime shipped.
type telemetryBackend struct {
	mu      sync.Mutex
	traces  []traceRecord
	logs    []json.RawMessage
	arrived chan struct{}
}

func newTelemetryBackend() *telemetryBackend {
	return &telemetryBackend{arrived: make(chan struct{}, 64)}
}

func (b *telemetryBackend) install(f *fake) {
	f.mux.HandleFunc("POST /v1/traces", func(w http.ResponseWriter, r *http.Request) {
		var in traceBatch
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		b.traces = append(b.traces, in.Records...)
		b.mu.Unlock()
		b.arrived <- struct{}{}
		w.WriteHeader(http.StatusAccepted)
	})
	f.mux.HandleFunc("POST /v1/logs", func(w http.ResponseWriter, r *http.Request) {
		var in logRecordBatch
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		b.logs = append(b.logs, in.Records...)
		b.mu.Unlock()
		b.arrived <- struct{}{}
		w.WriteHeader(http.StatusAccepted)
	})
}

func (b *telemetryBackend) traceCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.traces)
}

// A runtime that did not ask for traces publishes nothing at all, and does no
// setup with a side effect — what core.TraceOptions requires.
func TestTracingOffPublishesNothing(t *testing.T) {
	f := newFake(t, fullDiscovery())
	svc := newTestServices(t, f, nil)
	if svc.Traces().Enabled() {
		t.Fatal("Traces().Enabled() = true with tracing off")
	}
}

// A platform that does not accept traces gets none, however the runtime was
// configured.
func TestTracesUnsupportedIsTheNoopPublisher(t *testing.T) {
	doc := fullDiscovery()
	doc.Features.Traces.Supported = false
	pub := newTracePublisher(nil, Config{}, core.TraceOptions{Enabled: true}, doc.Features.Traces)
	if pub.Enabled() {
		t.Fatal("a publisher was built for a platform that accepts no traces")
	}
}

// Records are batched and shipped, carrying the identity a consumer attributes
// them by.
func TestTraceSinkShipsBatches(t *testing.T) {
	f := newFake(t, fullDiscovery())
	b := newTelemetryBackend()
	b.install(f)
	svc := newTestServices(t, f, nil)

	sink := newTraceSink(svc.client, svc.cfg, traceFeature{MaxBatch: 2})
	for i := range 2 {
		if err := sink.Write(types.TraceEvent{Seq: uint64(i), Kind: "block"}); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if got := b.traceCount(); got != 2 {
		t.Fatalf("shipped %d records, want the full batch of 2", got)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.traces[0].DeploymentID != "test-deployment" || b.traces[0].Instance != "test-instance" {
		t.Fatalf("record = %+v, want the deployment and instance stamped on it", b.traces[0])
	}
}

// An idle runtime must not leave its last few records unwritten, which is what
// Flush is for — the publisher calls it whenever its queue drains.
func TestTraceSinkFlushesAPartialBatch(t *testing.T) {
	f := newFake(t, fullDiscovery())
	b := newTelemetryBackend()
	b.install(f)
	svc := newTestServices(t, f, nil)

	sink := newTraceSink(svc.client, svc.cfg, traceFeature{MaxBatch: 100})
	if err := sink.Write(types.TraceEvent{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if got := b.traceCount(); got != 0 {
		t.Fatalf("shipped %d records before a flush, want 0", got)
	}
	if err := sink.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := b.traceCount(); got != 1 {
		t.Fatalf("shipped %d records after Flush, want 1", got)
	}
}

// Retrying telemetry means holding records while more arrive, whose failure mode
// is unbounded memory in a runtime whose platform is already unwell. The buffer
// is cleared either way.
func TestTraceSinkDropsRatherThanAccumulates(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("POST /v1/traces", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	})
	svc := newTestServices(t, f, nil)

	sink := newTraceSink(svc.client, svc.cfg, traceFeature{MaxBatch: 1})
	if err := sink.Write(types.TraceEvent{Seq: 1}); err == nil {
		t.Fatal("Write err = nil, want the failure surfaced to the drain goroutine")
	}
	if len(sink.pending) != 0 {
		t.Fatalf("pending = %d records after a failed ship, want 0", len(sink.pending))
	}
}

// A platform that accepts no logs gets a nil sink, which is how a module says it
// ships none — and what the runtime's tee already checks for.
func TestLogSinkNilWhenUnsupported(t *testing.T) {
	doc := fullDiscovery()
	doc.Features.Logs.Supported = false
	svc := newTestServices(t, newFake(t, doc), nil)

	if svc.LogSink() != nil {
		t.Fatal("LogSink is non-nil for a platform that accepts no logs")
	}
}

// A shipped log record carries the identity a consumer attributes it by, and the
// sink imposes no level of its own so it never filters more than the handler it
// is teed with.
func TestLogSinkShipsRecords(t *testing.T) {
	f := newFake(t, fullDiscovery())
	b := newTelemetryBackend()
	b.install(f)
	svc := newTestServices(t, f, nil)

	sink := svc.LogSink()
	if sink == nil {
		t.Fatal("LogSink is nil for a platform that accepts logs")
	}
	slog.New(sink).Debug("a debug line", "order", "A-1")

	select {
	case <-b.arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("no log record was shipped")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var record map[string]any
	if err := json.Unmarshal(b.logs[0], &record); err != nil {
		t.Fatal(err)
	}
	if record["msg"] != "a debug line" || record["order"] != "A-1" {
		t.Fatalf("record = %v", record)
	}
	if record["deploymentId"] != "test-deployment" {
		t.Fatalf("record = %v, want the deployment stamped on it", record)
	}
}

// A platform hiccup must never fail a caller's log call.
func TestLogShippingFailureIsSwallowed(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("POST /v1/logs", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	})
	svc := newTestServices(t, f, nil)

	// A handler that returned an error here would surface it through slog, which
	// has nowhere sensible to put it.
	slog.New(svc.LogSink()).Info("still fine")
}

// A log call must not wait on the network. Logging happens from wherever the
// runtime is, a flow's own goroutine included, and a handler that made the
// request inline would put every one of those behind a slow platform.
func TestLogCallDoesNotBlockOnTheNetwork(t *testing.T) {
	f := newFake(t, fullDiscovery())
	release := make(chan struct{})
	f.mux.HandleFunc("POST /v1/logs", func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusAccepted)
	})
	t.Cleanup(func() { close(release) })
	svc := newTestServices(t, f, nil)

	logger := slog.New(svc.LogSink())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10 {
			logger.Info("a line")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a log call blocked on a platform that had not answered")
	}
}

// The queue drops rather than blocking or growing: a platform that is not
// answering must not become a runtime that runs out of memory.
func TestLogQueueDropsWhenFull(t *testing.T) {
	f := newFake(t, fullDiscovery())
	release := make(chan struct{})
	f.mux.HandleFunc("POST /v1/logs", func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.WriteHeader(http.StatusAccepted)
	})
	t.Cleanup(func() { close(release) })
	svc := newTestServices(t, f, nil)

	logger := slog.New(svc.LogSink())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range logQueueDepth * 3 {
			logger.Info("a line")
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("logging blocked once the queue filled, rather than dropping")
	}
}
