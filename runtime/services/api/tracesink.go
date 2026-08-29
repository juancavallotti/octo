package api

import (
	"context"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Batching defaults for the trace sink, used when the platform declares none.
const (
	defaultTraceBatch = 256
	defaultTraceFlush = 2 * time.Second
	maxTraceBatch     = 10000
	// shipTimeout bounds one shipping request. It is short: telemetry that is
	// slow to accept is telemetry to drop, not to wait on.
	shipTimeout = 10 * time.Second
)

// traceRecord is what goes on the wire. The embedded event flattens into the
// same JSON object, so a consumer sees one flat record rather than an envelope to
// unwrap — the shape the k8s module already publishes.
type traceRecord struct {
	types.TraceEvent
	DeploymentID string `json:"deploymentId,omitempty"`
	Instance     string `json:"instance,omitempty"`
}

type traceBatch struct {
	Records []traceRecord `json:"records"`
}

// traceSink ships trace records to the platform API in batches.
//
// It needs no locking: core.TraceSink guarantees every method is called from the
// publisher's single drain goroutine.
type traceSink struct {
	c            *client
	deploymentID string
	instance     string
	maxBatch     int
	interval     time.Duration

	pending  []traceRecord
	lastSend time.Time
}

func newTraceSink(c *client, cfg Config, f traceFeature) *traceSink {
	maxBatch := f.MaxBatch
	if maxBatch <= 0 {
		maxBatch = defaultTraceBatch
	}
	maxBatch = min(maxBatch, maxTraceBatch)
	interval := defaultTraceFlush
	if f.FlushIntervalMillis > 0 {
		interval = time.Duration(f.FlushIntervalMillis) * time.Millisecond
	}
	return &traceSink{
		c: c, deploymentID: cfg.DeploymentID, instance: cfg.InstanceID,
		maxBatch: maxBatch, interval: interval, lastSend: time.Now(),
	}
}

// Write buffers one record, shipping when the batch is full or the interval has
// passed.
func (s *traceSink) Write(event types.TraceEvent) error {
	s.pending = append(s.pending, traceRecord{
		TraceEvent: event, DeploymentID: s.deploymentID, Instance: s.instance,
	})
	if len(s.pending) >= s.maxBatch || time.Since(s.lastSend) >= s.interval {
		return s.ship()
	}
	return nil
}

// Flush ships whatever is held. The publisher calls it whenever its queue drains,
// so an idle runtime does not leave its last few records unwritten.
func (s *traceSink) Flush() error { return s.ship() }

// Close ships the remainder. This is the flush worth having: an instance being
// rolled has seconds to hand over what it saw.
func (s *traceSink) Close() error { return s.ship() }

// ship sends the buffered records.
//
// The buffer is cleared whether or not the send succeeded. Retrying telemetry
// means holding records while more arrive, and the failure mode of that is
// unbounded memory in a runtime whose platform is already unwell.
func (s *traceSink) ship() error {
	if len(s.pending) == 0 {
		return nil
	}
	batch := traceBatch{Records: s.pending}
	s.pending = nil
	s.lastSend = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), shipTimeout)
	defer cancel()
	return s.c.json(ctx, routeTraces, s.c.url(routeTraces), batch, nil, shipTimeout)
}

// newTracePublisher builds the module's trace publisher: the no-op when tracing
// is off, so a runtime that did not ask for traces publishes nothing at all — and
// does no setup with a side effect, as core.TraceOptions requires.
//
//nolint:ireturn // returns the TracePublisher interface Services.Traces exposes
func newTracePublisher(c *client, cfg Config, opts core.TraceOptions, f traceFeature) core.TracePublisher {
	if !opts.Enabled || !f.Supported {
		return core.NoopTracer()
	}
	return core.NewBufferedTracer(newTraceSink(c, cfg, f), opts)
}
