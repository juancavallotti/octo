package core

import (
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/juancavallotti/octo/runtime/types"
)

// defaultTraceBuffer is how many records a publisher holds when the caller did
// not choose. It is sized to absorb a burst, not a sustained overload: no buffer
// makes a single drain goroutine keep up with every flow worker in the process,
// it only sets how long saturation takes to start dropping.
const defaultTraceBuffer = 4096

// TracePublisher is the seam any component publishes a trace record through. It
// is looked up process-wide with Tracer, so a block deep in the engine or a
// source with no runtime-services context reaches the same publisher without
// either of them being plumbed to it.
//
// Enabled is separate from Publish, and every caller is expected to check it
// first, because building a record is the expensive part: a caller that would
// have to marshal a body or walk a map should find out it is wasting the work
// before it does it, not after. It is one atomic load.
//
// Publish must not block. The implementation this package provides hands the
// record to a goroutine of its own and drops when that queue is full, so a
// tracer can never put a flow worker behind a disk write or a broker.
type TracePublisher interface {
	// Enabled reports whether records are being kept. When false, Publish is a
	// no-op and callers should skip building the record at all.
	Enabled() bool
	// Publish records one event. Seq is assigned by the publisher; whatever the
	// caller set is overwritten.
	Publish(event types.TraceEvent)
}

// TraceSink is where a runtime-services module writes the records the publisher
// drains. It is the one part of tracing that differs per module: the standalone
// module writes a file, the k8s module publishes to a subject.
//
// Every method is called only from the publisher's single drain goroutine, so an
// implementation needs no locking of its own.
type TraceSink interface {
	// Write records one event.
	Write(event types.TraceEvent) error
	// Flush pushes anything buffered to the underlying destination. It is called
	// whenever the publisher runs out of queued records, so a sink that buffers
	// does not leave the last few records of an idle runtime unwritten. A sink
	// with nothing to flush returns nil.
	Flush() error
	// Close flushes and releases the destination. It is called once, after the
	// drain goroutine has stopped, so it may assume no concurrent Write.
	Close() error
}

// TraceOptions is the runtime's tracing configuration, resolved from the `octo
// run` flags and their environment defaults and handed to the services module at
// construction.
//
// A module reads what applies to it and ignores the rest — the standalone module
// uses File, the k8s module publishes to a subject and has none — in the same way
// both already treat services.Options.ResourceRoot. Bodies, Vars and MaxPayload
// are not read by a module at all: they configure what the listeners capture, and
// travel here so the whole feature has one configuration value rather than two.
type TraceOptions struct {
	// Enabled is the master switch. When false a module returns NoopTracer and
	// must not do any setup that has a side effect — notably, it must not create
	// an output file.
	Enabled bool
	// File is the requested output path for a module that writes one. Empty
	// lets the module choose its own default.
	File string
	// Buffer is how many records the publisher queues before it starts dropping.
	// Zero means defaultTraceBuffer.
	Buffer int
	// Bodies and Vars say whether a captured record carries the message's
	// payload and variables.
	Bodies bool
	Vars   bool
	// MaxPayload caps a captured payload in bytes. A payload over the cap is
	// omitted whole rather than cut, because a sliced JSON document is not JSON.
	MaxPayload int
}

// noopTracer keeps nothing. It is what Tracer answers with until a real
// publisher is installed, and what a module returns when tracing is off, so no
// caller ever has to nil-check.
type noopTracer struct{}

func (noopTracer) Enabled() bool              { return false }
func (noopTracer) Publish(_ types.TraceEvent) {}

// noopTracerInstance is shared: it holds nothing, so one is enough.
var noopTracerInstance TracePublisher = noopTracer{}

// NoopTracer returns the publisher that keeps nothing. A module whose tracing is
// disabled returns this rather than nil.
//
//nolint:ireturn // returns the TracePublisher interface intentionally
func NoopTracer() TracePublisher { return noopTracerInstance }

// tracerRef is a one-field holder so the installed publisher can live in an
// atomic.Pointer. An interface cannot go into one directly, and atomic.Value is
// not an option either: it panics when the concrete type changes, which is
// exactly what installing a real publisher over the no-op does.
type tracerRef struct {
	publisher TracePublisher
}

// installedTracer is the process-wide publisher. It is a package-level var
// rather than an init(), which the module-manifest lint reserves for loadable
// modules.
var installedTracer = func() *atomic.Pointer[tracerRef] {
	p := &atomic.Pointer[tracerRef]{}
	p.Store(&tracerRef{publisher: noopTracerInstance})
	return p
}()

// Tracer returns the process-wide trace publisher. It never returns nil: until
// something calls SetTracer it is the no-op, whose Enabled is false, so a caller
// guarded by `if core.Tracer().Enabled()` costs one atomic load and one
// non-virtual comparison when tracing is off.
//
//nolint:ireturn // returns the TracePublisher interface intentionally
func Tracer() TracePublisher {
	return installedTracer.Load().publisher
}

// SetTracer installs the process-wide publisher, normally the one the active
// runtime-services module built (RuntimeServices.Traces). A nil publisher
// re-installs the no-op, so tracing can be turned off without leaving a dangling
// reference.
func SetTracer(publisher TracePublisher) {
	if publisher == nil {
		publisher = noopTracerInstance
	}
	installedTracer.Store(&tracerRef{publisher: publisher})
}

// BufferedTracer is the driver every module shares: a queue, a goroutine that
// drains it into the module's sink, and the accounting that makes a loss
// visible. A module supplies the sink and nothing else.
//
// It is exported because a module holds the concrete type to Close it — the
// TracePublisher interface it satisfies is deliberately just the two methods the
// hot path needs.
type BufferedTracer struct {
	sink TraceSink

	// records is the queue between the flow workers publishing and the single
	// goroutine writing. It is never closed: closing it would race a Publish
	// that has already passed the open check, and a panicking send is a worse
	// outcome than a record nobody drains.
	records chan types.TraceEvent
	// open gates Publish. It goes false in Close, before the drain is asked to
	// stop, so publishes stop arriving before writes stop happening.
	open atomic.Bool
	// quit asks the drain to finish, and drained is closed once it has.
	quit    chan struct{}
	drained chan struct{}
	once    sync.Once

	seq     atomic.Uint64
	emitted atomic.Uint64
	dropped atomic.Uint64
	failed  atomic.Uint64

	// reported is how many drops the drain has already announced in-band, so a
	// marker is written per new gap rather than per record after one.
	reported uint64
	// wroteError records that a write failure has already been logged. The
	// first is worth a line; a sink that fails once usually fails for every
	// record after it, and a line each would bury the run's real output. The
	// total is reported on Close instead.
	wroteError bool
}

// NewBufferedTracer returns a publisher that queues records and drains them into
// sink on a goroutine of its own.
//
// The queue belongs to the tracer rather than to the runtime deliberately. The
// block-event dispatcher used to own one and had it removed (see blockevents.go):
// with every flow worker producing and one goroutine consuming, no buffer size
// makes the consumer keep up with sustained load, so what a buffer really sets is
// how long saturation takes to start dropping. That trade is the tracer's to make
// and to report, not something to impose on every listener in the process.
func NewBufferedTracer(sink TraceSink, opts TraceOptions) *BufferedTracer {
	size := opts.Buffer
	if size <= 0 {
		size = defaultTraceBuffer
	}
	t := &BufferedTracer{
		sink:    sink,
		records: make(chan types.TraceEvent, size),
		quit:    make(chan struct{}),
		drained: make(chan struct{}),
	}
	t.open.Store(true)
	go t.drain()
	return t
}

// Enabled reports whether the tracer is still accepting records.
func (t *BufferedTracer) Enabled() bool { return t.open.Load() }

// Publish stamps the record with its sequence number and queues it, dropping it
// when the queue is full.
//
// Every record takes a sequence number, including one that is then dropped, so
// the gap a drop leaves is visible as a gap in Seq in the records that survived
// — the marker the drain writes says how many were lost, and the numbering says
// where.
func (t *BufferedTracer) Publish(event types.TraceEvent) {
	if !t.open.Load() {
		return
	}
	event.Seq = t.seq.Add(1)
	select {
	case t.records <- event:
	default:
		t.dropped.Add(1)
	}
}

// Emitted returns how many records reached the sink.
func (t *BufferedTracer) Emitted() uint64 { return t.emitted.Load() }

// Dropped returns how many records the queue could not hold. Non-zero means the
// trace is incomplete, which is why it is also announced in-band.
func (t *BufferedTracer) Dropped() uint64 { return t.dropped.Load() }

// Close stops accepting records, drains what is queued, and closes the sink. It
// is safe to call more than once.
func (t *BufferedTracer) Close() error {
	var err error
	t.once.Do(func() {
		t.open.Store(false)
		close(t.quit)
		<-t.drained

		slog.Info("tracing stopped",
			"emitted", t.emitted.Load(),
			"dropped", t.dropped.Load(),
			"writeErrors", t.failed.Load(),
			"listenerPanics", DefaultBlockEvents().Panics())

		err = t.sink.Close()
	})
	return err
}

// drain writes queued records until asked to stop, flushing whenever it runs out
// of work. Flushing on the way to idle rather than on a timer is what makes a
// local run's trace file readable the moment the runtime goes quiet, without a
// ticker whose period is one more thing to be wrong.
func (t *BufferedTracer) drain() {
	defer close(t.drained)
	for {
		// Take everything already queued before considering a flush: under load
		// this is the whole loop, and the flush below never runs.
		select {
		case event := <-t.records:
			t.write(event)
			continue
		default:
		}

		t.flush()

		select {
		case event := <-t.records:
			t.write(event)
		case <-t.quit:
			t.drainRemaining()
			return
		}
	}
}

// drainRemaining writes what is still queued at shutdown and flushes once. It
// does not wait for more: Publish has already been closed, so what is here is
// all there will be.
func (t *BufferedTracer) drainRemaining() {
	for {
		select {
		case event := <-t.records:
			t.write(event)
		default:
			t.flush()
			return
		}
	}
}

// write emits one record, preceded by a drop marker when records have been lost
// since the last one.
//
// Only the record itself counts towards Emitted; the marker is bookkeeping about
// the stream rather than something that happened in a flow. That is what keeps
// Emitted plus Dropped equal to what was published, which is the arithmetic
// anyone auditing a trace for completeness will do.
func (t *BufferedTracer) write(event types.TraceEvent) {
	t.announceDrops()
	if t.writeOne(event) {
		t.emitted.Add(1)
	}
}

// announceDrops writes the in-band marker for any records lost since the last
// one, so a reader of the stream alone can tell it is incomplete.
func (t *BufferedTracer) announceDrops() {
	total := t.dropped.Load()
	if total == t.reported {
		return
	}
	lost := total - t.reported
	t.reported = total
	t.writeOne(types.TraceEvent{
		Seq:  t.seq.Add(1),
		Kind: types.TraceDropped,
		Attrs: map[string]any{
			"dropped": lost,
			"total":   total,
		},
	})
}

// writeOne hands a record to the sink and says whether it landed, counting a
// failure rather than propagating it: there is nobody above the drain goroutine
// to handle it, and a broken sink must not become a broken runtime.
func (t *BufferedTracer) writeOne(event types.TraceEvent) bool {
	if err := t.sink.Write(event); err != nil {
		t.failed.Add(1)
		if !t.wroteError {
			t.wroteError = true
			slog.Error("tracing: sink write failed, further failures are counted only",
				"kind", event.Kind, "error", err)
		}
		return false
	}
	return true
}

// flush pushes buffered records through, counting a failure the same way write
// does.
func (t *BufferedTracer) flush() {
	if err := t.sink.Flush(); err != nil && !t.wroteError {
		t.wroteError = true
		slog.Error("tracing: sink flush failed, further failures are counted only", "error", err)
	}
}
