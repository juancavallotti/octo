package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// Shipping bounds. The queue is deliberately small: it exists to decouple a log
// call from a network round trip, not to survive an outage, and a runtime that
// buffered megabytes of logs for a platform that is not answering would trade a
// telemetry problem for a memory one.
const (
	logQueueDepth = 1024
	logBatchMax   = 128
	logFlushEvery = time.Second
	// logDrainTimeout bounds the final flush at shutdown. An instance being rolled
	// has seconds to hand over what it saw.
	logDrainTimeout = 5 * time.Second
)

// Services ships logs when the platform accepts them, so the runtime tees its
// loggers through it.
var _ core.LogShipper = (*Services)(nil)

// LogSink returns the handler that ships log records to the platform API, or nil
// when the platform does not accept them — which is how a module says it ships no
// logs, and what teeDefaultLoggerToSink already checks for.
//
//nolint:ireturn // satisfies core.LogShipper
func (s *Services) LogSink() slog.Handler { return s.logSink }

// newLogSink builds an slog.Handler that ships every record to the platform as
// one JSON object per record, tagged with the deployment and instance it came
// from, and the shipper that drains them.
//
// It is a plain JSON handler over a queueing writer, so slog does the
// level/attr/group formatting and the identity rides along as base attributes.
// The threshold is debug so it never filters more than the destination it is teed
// with: the console handler keeps applying its own level, while the platform
// captures full fidelity.
//
//nolint:ireturn // returns the slog.Handler interface intentionally
func newLogSink(c *client, cfg Config) (slog.Handler, *logShipper) {
	shipper := newLogShipper(c)
	h := slog.NewJSONHandler(logWriter{shipper: shipper}, &slog.HandlerOptions{Level: slog.LevelDebug})
	return h.WithAttrs([]slog.Attr{
		slog.String("deploymentId", cfg.DeploymentID),
		slog.String("instance", cfg.InstanceID),
	}), shipper
}

// logRecordBatch is what goes on the wire. Each record is the raw JSON slog
// produced, so the platform sees the same object it would have read from a file.
type logRecordBatch struct {
	Records []json.RawMessage `json:"records"`
}

// logWriter hands each Write — one slog JSON record — to the shipper and returns.
//
// It must not do the network call itself. A log statement is written from
// wherever the runtime happens to be, including a flow's own goroutine, and a
// handler that made an HTTP request inline would put every one of those behind a
// platform that is slow to answer. The publish error is dropped for the same
// reason a shipping hiccup must never fail a caller's log call — there is nowhere
// to report it that would not itself be a log call.
type logWriter struct{ shipper *logShipper }

// Write queues a copy of p. slog reuses its formatting buffer after Write
// returns, so the bytes are copied to keep them stable for the request.
func (w logWriter) Write(p []byte) (int, error) {
	record := make([]byte, len(p))
	copy(record, p)
	w.shipper.enqueue(record)
	return len(p), nil
}

// logShipper batches queued records and sends them on its own goroutine.
type logShipper struct {
	c     *client
	queue chan json.RawMessage
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

func newLogShipper(c *client) *logShipper {
	s := &logShipper{
		c:     c,
		queue: make(chan json.RawMessage, logQueueDepth),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go s.run()
	return s
}

// enqueue queues a record, dropping it when the queue is full.
//
// Dropping is the right failure here and the only one available: blocking would
// stall whatever was logging, and growing would turn a platform that is not
// answering into a runtime that runs out of memory.
func (s *logShipper) enqueue(record json.RawMessage) {
	select {
	case s.queue <- record:
	default:
	}
}

// run drains the queue, sending on a full batch or on the flush interval.
func (s *logShipper) run() {
	defer close(s.done)
	ticker := time.NewTicker(logFlushEvery)
	defer ticker.Stop()

	batch := make([]json.RawMessage, 0, logBatchMax)
	for {
		select {
		case record := <-s.queue:
			batch = append(batch, record)
			if len(batch) >= logBatchMax {
				batch = s.ship(batch)
			}
		case <-ticker.C:
			batch = s.ship(batch)
		case <-s.stop:
			// Take whatever is already queued before going, so the last thing a
			// rolled instance logged is not the thing nobody ever sees.
			batch = s.drain(batch)
			s.ship(batch)
			return
		}
	}
}

// drain moves everything already queued into the batch.
func (s *logShipper) drain(batch []json.RawMessage) []json.RawMessage {
	for {
		select {
		case record := <-s.queue:
			batch = append(batch, record)
		default:
			return batch
		}
	}
}

// ship sends a batch and returns an empty one. The error is deliberately
// discarded: see logWriter.
func (s *logShipper) ship(batch []json.RawMessage) []json.RawMessage {
	if len(batch) == 0 {
		return batch
	}
	ctx, cancel := context.WithTimeout(context.Background(), shipTimeout)
	defer cancel()
	_ = s.c.json(ctx, routeLogs, s.c.url(routeLogs),
		logRecordBatch{Records: batch}, nil, shipTimeout)
	return batch[:0]
}

// close stops the shipper after a final flush, bounded so shutdown cannot hang on
// a platform that has stopped answering.
func (s *logShipper) close() {
	s.once.Do(func() {
		close(s.stop)
		select {
		case <-s.done:
		case <-time.After(logDrainTimeout):
		}
	})
}
