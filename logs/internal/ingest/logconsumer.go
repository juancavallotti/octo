package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go"
)

// LogConsumer subscribes to LogSubject as a competing consumer and persists each
// delivered record through a LogStore. The subscription callback only enqueues, so a
// slow store never stalls NATS delivery; a bounded worker pool does the inserts.
type LogConsumer struct {
	store   LogStore
	workers int
}

// NewLogConsumer returns a consumer that persists records through store using the
// given number of insert workers (clamped to at least one).
func NewLogConsumer(store LogStore, workers int) *LogConsumer {
	if workers < 1 {
		workers = 1
	}
	return &LogConsumer{store: store, workers: workers}
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

	work := make(chan *nats.Msg, c.workers)
	var wg sync.WaitGroup
	wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-subCtx.Done():
					return
				case m := <-work:
					c.handle(subCtx, m)
				}
			}
		}()
	}

	sub, err := conn.QueueSubscribe(LogSubject, logQueueGroup, func(m *nats.Msg) {
		select {
		case work <- m:
		case <-subCtx.Done():
		}
	})
	if err != nil {
		cancel()
		wg.Wait()
		return nil, fmt.Errorf("ingest: subscribe %q: %w", LogSubject, err)
	}
	return &Subscription{sub: sub, cancel: cancel, wg: &wg}, nil
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
