package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Attribute keys a source record carries. They are constants because the same
// three appear on both sides of a request, and a reader joining receive to
// respond has to see the same spelling on each.
const (
	attrMethod = "method"
	attrRoute  = "route"
	attrStatus = "status"
	attrStream = "stream"
)

// traceRequest opens the trace for one inbound request and returns the writer to
// serve it with and the function that closes the trace.
//
// It names the trace here rather than leaving it to the flow boundary so the
// source's own two records share the id with everything the message goes on to
// touch — the request is where the work genuinely began, and a trace that starts
// one block later cannot show the time spent before it.
//
// When tracing is off both returns are the originals: the caller's writer
// unwrapped, and a function that does nothing.
func (s *source) traceRequest(w http.ResponseWriter, r *http.Request, msg *types.Message) (
	http.ResponseWriter, func(),
) {
	tracer := core.Tracer()
	if !tracer.Enabled() {
		return w, func() {}
	}
	if _, err := msg.EnsureTraceID(); err != nil {
		// Losing the id costs the trace its grouping, not the caller their request.
		slog.Warn("http source: could not generate a trace id", "error", err)
		return w, func() {}
	}

	started := time.Now()
	record := types.TraceEvent{
		Kind:          types.TraceSourceReceive,
		TraceID:       msg.TraceID(),
		EventID:       msg.EventID,
		CorrelationID: msg.CorrelationID,
		OccurredAt:    started,
		Attrs: map[string]any{
			attrMethod: r.Method,
			// The route pattern, not the request path: the path carries whatever
			// the caller put in it, and grouping a trace by "/orders/{id}" is what
			// makes several requests comparable.
			attrRoute: s.pattern,
		},
	}
	tracer.Publish(record)

	// The status is captured rather than derived, because most of the ways a
	// request ends here never see a flow result: a timeout, a client that hung up,
	// a body that would not parse. Those are the responses most worth having in a
	// trace, and the only place they are all visible is the writer.
	recorder := &statusRecorder{ResponseWriter: w}
	return recorder, func() {
		tracer.Publish(types.TraceEvent{
			Kind:          types.TraceSourceRespond,
			TraceID:       msg.TraceID(),
			EventID:       msg.EventID,
			CorrelationID: msg.CorrelationID,
			OccurredAt:    time.Now(),
			DurationNs:    time.Since(started).Nanoseconds(),
			Attrs: map[string]any{
				attrMethod: r.Method,
				attrRoute:  s.pattern,
				attrStatus: recorder.statusCode(),
			},
		})
	}
}

// traceStream records a stream being opened. An SSE response has no single status
// and no end worth waiting for — a stream lives until the client leaves — so it
// gets the admission record and no response record.
func (s *source) traceStream(r *http.Request, msg *types.Message) {
	tracer := core.Tracer()
	if !tracer.Enabled() {
		return
	}
	if _, err := msg.EnsureTraceID(); err != nil {
		slog.Warn("http source: could not generate a trace id", "error", err)
		return
	}
	tracer.Publish(types.TraceEvent{
		Kind:          types.TraceSourceReceive,
		TraceID:       msg.TraceID(),
		EventID:       msg.EventID,
		CorrelationID: msg.CorrelationID,
		OccurredAt:    time.Now(),
		Attrs: map[string]any{
			attrMethod: r.Method,
			attrRoute:  s.pattern,
			attrStream: true,
		},
	})
}

// statusRecorder remembers the status a handler wrote, so the trace can report
// what the caller actually received.
//
// It is installed only while tracing is on: an untraced request is served through
// the caller's own writer, with no wrapper in the way.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the first status written. Only the first counts, because
// that is the one that reached the client — a second call is a bug net/http
// already logs.
func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

// Write records the implicit 200 that writing a body without a status produces.
//
//nolint:wrapcheck // a transparent ResponseWriter must pass its writer's error through unchanged
func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap exposes the wrapped writer to http.ResponseController, so flushing and
// deadline control still reach the real one.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// statusCode is what the client received, or 0 when the handler wrote nothing at
// all — which is itself worth seeing in a trace, since it means the client
// disconnected before there was anything to send.
func (r *statusRecorder) statusCode() int { return r.status }
