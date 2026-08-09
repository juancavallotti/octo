package http

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// traceCollector keeps what a source published.
type traceCollector struct {
	mu      sync.Mutex
	records []types.TraceEvent
}

func (c *traceCollector) Enabled() bool { return true }

func (c *traceCollector) Publish(event types.TraceEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, event)
}

func (c *traceCollector) all() []types.TraceEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]types.TraceEvent(nil), c.records...)
}

func (c *traceCollector) of(t *testing.T, kind types.TraceKind) types.TraceEvent {
	t.Helper()
	for _, record := range c.all() {
		if record.Kind == kind {
			return record
		}
	}
	t.Fatalf("no %s record in %d records", kind, len(c.all()))
	return types.TraceEvent{}
}

func tracingHTTP(t *testing.T) *traceCollector {
	t.Helper()
	c := &traceCollector{}
	previous := core.Tracer()
	core.SetTracer(c)
	t.Cleanup(func() { core.SetTracer(previous) })
	return c
}

func traceTestMessage(t *testing.T) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("req-1")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	return msg
}

// The source names the trace at admission so its own records share the id with
// everything the message goes on to touch. A trace that started one block later
// could not show the time spent before it.
func TestTraceRequestRecordsAdmissionAndResponse(t *testing.T) {
	c := tracingHTTP(t)
	src := &source{pattern: "/orders/{id}"}
	msg := traceTestMessage(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders/42", http.NoBody)

	w, endTrace := src.traceRequest(rec, req, msg)
	w.WriteHeader(http.StatusCreated)
	endTrace()

	if msg.TraceID() == "" {
		t.Fatal("the source minted no trace id")
	}

	receive := c.of(t, types.TraceSourceReceive)
	if receive.TraceID != msg.TraceID() || receive.EventID != msg.EventID {
		t.Errorf("receive record identity = %q/%q", receive.TraceID, receive.EventID)
	}
	if receive.CorrelationID != "req-1" {
		t.Errorf("CorrelationID = %q, want the caller's", receive.CorrelationID)
	}
	// The route pattern, not the request path: grouping by "/orders/{id}" is what
	// makes several requests comparable.
	if got := receive.Attrs["route"]; got != "/orders/{id}" {
		t.Errorf("route = %v, want the pattern", got)
	}
	if got := receive.Attrs["method"]; got != http.MethodPost {
		t.Errorf("method = %v", got)
	}

	respond := c.of(t, types.TraceSourceRespond)
	if respond.TraceID != msg.TraceID() {
		t.Errorf("the two records disagree on the trace: %q vs %q", receive.TraceID, respond.TraceID)
	}
	if got := respond.Attrs["status"]; got != http.StatusCreated {
		t.Errorf("status = %v, want 201", got)
	}
	if respond.DurationNs <= 0 {
		t.Errorf("DurationNs = %d, want the time the request took", respond.DurationNs)
	}
}

// Most of the ways a request ends never see a flow result — a timeout, a body
// that would not parse, a client that hung up. Those are the responses most worth
// having in a trace, and the writer is the only place they are all visible.
func TestTraceRequestRecordsAResponseNoFlowProduced(t *testing.T) {
	c := tracingHTTP(t)
	src := &source{pattern: "/orders/{id}"}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/orders/42", http.NoBody)

	w, endTrace := src.traceRequest(rec, req, traceTestMessage(t))
	writeError(w, http.StatusGatewayTimeout, "flow timed out")
	endTrace()

	if got := c.of(t, types.TraceSourceRespond).Attrs["status"]; got != http.StatusGatewayTimeout {
		t.Errorf("status = %v, want 504", got)
	}
}

// A client that disconnects leaves nothing written at all, which is itself the
// finding — so the record says 0 rather than inventing a 200.
func TestTraceRequestRecordsNoStatusWhenNothingWasWritten(t *testing.T) {
	c := tracingHTTP(t)
	src := &source{pattern: "/orders/{id}"}

	_, endTrace := src.traceRequest(
		httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/orders/42", http.NoBody), traceTestMessage(t),
	)
	endTrace()

	if got := c.of(t, types.TraceSourceRespond).Attrs["status"]; got != 0 {
		t.Errorf("status = %v, want 0 for a request nothing answered", got)
	}
}

// With tracing off the caller's own writer is served through — no wrapper in the
// way, no id on the message, nothing published.
func TestTraceRequestIsTransparentWhenTracingIsOff(t *testing.T) {
	c := tracingHTTP(t)
	core.SetTracer(nil)

	src := &source{pattern: "/orders/{id}"}
	msg := traceTestMessage(t)
	rec := httptest.NewRecorder()

	w, endTrace := src.traceRequest(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", http.NoBody), msg)
	endTrace()

	if w != http.ResponseWriter(rec) {
		t.Error("an untraced request was served through a wrapper")
	}
	if msg.TraceID() != "" {
		t.Errorf("an untraced request minted a trace id: %q", msg.TraceID())
	}
	if got := len(c.all()); got != 0 {
		t.Errorf("records = %d with tracing off, want 0", got)
	}
}

// A stream has no single status and no end worth waiting for, so it gets the
// admission record and no response record.
func TestTraceStreamRecordsTheOpen(t *testing.T) {
	c := tracingHTTP(t)
	src := &source{pattern: "/events"}
	msg := traceTestMessage(t)

	src.traceStream(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/events", http.NoBody), msg)

	receive := c.of(t, types.TraceSourceReceive)
	if got := receive.Attrs["stream"]; got != true {
		t.Errorf("stream = %v, want true", got)
	}
	for _, record := range c.all() {
		if record.Kind == types.TraceSourceRespond {
			t.Error("a stream produced a response record")
		}
	}
}

// The recorder must not change what the client receives, only observe it.
func TestStatusRecorderIsTransparent(t *testing.T) {
	rec := httptest.NewRecorder()
	w := &statusRecorder{ResponseWriter: rec}

	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := rec.Body.String(); got != "hello" {
		t.Errorf("body = %q", got)
	}
	// Writing a body without a status is an implicit 200, and the record has to
	// say so rather than reporting nothing was written.
	if w.statusCode() != http.StatusOK {
		t.Errorf("status = %d, want the implicit 200", w.statusCode())
	}
	if w.Unwrap() != http.ResponseWriter(rec) {
		t.Error("Unwrap did not expose the wrapped writer")
	}
}
