package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	// logBuffer is how many delivered records may wait to be written.
	//
	// It was the worker count — eight — which is not a buffer so much as one slot
	// per worker, and the callback blocked when it filled. Blocking there does not
	// stall the other subscriptions, but it backs records up in this one's pending
	// queue until the client drops them as a slow consumer: the same loss, arriving
	// as a client-side error that names neither a count nor an app. This is sized
	// to absorb a burst instead, and past it the consumer sheds and says so.
	//
	// Matched to traceBuffer deliberately. A log line is smaller than a trace
	// record and a request produces fewer of them, so at the same depth the log
	// path has strictly more slack than the trace path — which is the right way
	// round for the records an operator reads when something is going wrong.
	logBuffer = 8192

	// logWriteTimeout bounds one insert. It is a backstop against a database that
	// has stopped answering rather than a tuning knob — a healthy insert is well
	// under a millisecond — and it exists so a worker cannot park forever holding
	// up Close.
	logWriteTimeout = 30 * time.Second
)

// LogConsumer subscribes to LogSubject as a competing consumer and persists each
// delivered record through a LogStore. The subscription callback only enqueues, so a
// slow store never stalls NATS delivery; a bounded worker pool does the inserts.
type LogConsumer struct {
	shedder

	store   LogStore
	workers int
}

// NewLogConsumer returns a consumer that persists records through store using the
// given number of insert workers (clamped to at least one).
func NewLogConsumer(store LogStore, workers int) *LogConsumer {
	if workers < 1 {
		workers = 1
	}
	return &LogConsumer{shedder: shedder{what: "log"}, store: store, workers: workers}
}

// Subscription stops delivery and drains its workers on Close, so afterwards no
// insert is still in flight.
type Subscription struct {
	sub    *nats.Subscription
	cancel context.CancelFunc
	wg     *sync.WaitGroup
	once   sync.Once
}

// Start joins the LogSubject queue group and runs the worker pool until the
// returned Subscription is closed (or ctx is cancelled). Records are inserted
// best-effort: a decode or store error is logged and the record dropped, matching
// the at-most-once delivery of the runtime's core-NATS shipping.
func (c *LogConsumer) Start(ctx context.Context, conn *nats.Conn) (*Subscription, error) {
	subCtx, cancel := context.WithCancel(ctx)

	work := make(chan *nats.Msg, logBuffer)
	var wg sync.WaitGroup
	wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-subCtx.Done():
					c.drain(work)
					return
				case m := <-work:
					c.write(m)
				}
			}
		}()
	}

	sub, err := conn.QueueSubscribe(LogSubject, logQueueGroup, func(m *nats.Msg) {
		c.offer(work, m)
	})
	if err != nil {
		cancel()
		wg.Wait()
		return nil, fmt.Errorf("ingest: subscribe %q: %w", LogSubject, err)
	}
	return &Subscription{sub: sub, cancel: cancel, wg: &wg}, nil
}

// write persists one record under a context of its own, never the
// subscription's.
//
// A record that has reached this point is already off the wire, and core NATS
// will not redeliver it, so cancelling its insert loses it for good. Passing the
// subscription context down meant a record being written when a termination
// signal arrived had its insert aborted — and Close's wait for "in-flight
// inserts" was really a wait for them to fail fast. That is a lost log record on
// every rolling restart, which is every deploy.
//
// The subscription context still stops the loop above. It just no longer reaches
// into a write that has already started.
func (c *LogConsumer) write(m *nats.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), logWriteTimeout)
	defer cancel()
	c.handle(ctx, m)
}

// drain writes what is still buffered on the way out, within one shared budget.
//
// Workers used to return on cancellation and abandon whatever was still in the
// channel — records NATS considers delivered and will never send again. Every
// worker drains, so together they empty it; whichever finds it empty returns.
//
// The budget is bounded because a shutdown must not hang on a database that has
// gone away, and unbounded here means "until the kubelet's grace period runs out
// and SIGKILL arrives", which loses the records anyway and takes 30 seconds off
// every deploy to do it.
func (c *LogConsumer) drain(work <-chan *nats.Msg) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancel()
	for {
		select {
		case m := <-work:
			c.handle(ctx, m)
		default:
			return
		}
	}
}

// handle parses one delivered record and persists it, dropping (with a log) any
// record that fails to decode or store.
func (c *LogConsumer) handle(ctx context.Context, m *nats.Msg) {
	ev, err := parseLogEvent(m.Data)
	if err != nil {
		slog.Warn("ingest: drop undecodable record", "err", err)
		return
	}
	if err := c.store.Insert(ctx, ev); err != nil {
		slog.Error("ingest: store record", "deployment", ev.DeploymentID, "err", err)
	}
}

// Close stops delivery and waits for in-flight inserts to finish.
func (s *Subscription) Close() error {
	var unsubErr error
	s.once.Do(func() {
		unsubErr = s.sub.Unsubscribe()
		s.cancel()
		s.wg.Wait()
	})
	if unsubErr != nil {
		return fmt.Errorf("ingest: unsubscribe: %w", unsubErr)
	}
	return nil
}
