package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/juancavallotti/octo/logs/internal/cost"
)

// traceQueueGroup makes every aggregator replica a competing consumer of
// TraceSubject, so each record is delivered to exactly one replica.
const traceQueueGroup = "octo-traces"

const (
	// traceBuffer is how many delivered records may wait to be written.
	//
	// It exists to absorb a burst, not to survive an outage: at roughly two dozen
	// records per traced request it is a few hundred requests of slack. Past that
	// the consumer sheds load rather than growing without bound, because the
	// alternative — blocking in the NATS callback — stalls delivery for every
	// other subscriber on the connection.
	traceBuffer = 8192

	// traceBatchSize and traceBatchWait bound a write both ways, so a busy
	// deployment writes full batches and a quiet one still writes promptly rather
	// than holding its records until the next burst pushes them out.
	traceBatchSize = 500
	traceBatchWait = 50 * time.Millisecond

	// dropWarnInterval rate-limits the overflow warning. The condition that
	// produces it produces it thousands of times a second, and a log line per
	// dropped record turns shedding trace load into a second, worse flood.
	dropWarnInterval = 10 * time.Second

	// shutdownFlushTimeout bounds the last write on the way out, for both
	// consumers. Records held at that point are already off the wire and will not
	// be redelivered, so they are worth waiting for — but not worth hanging a
	// shutdown over.
	shutdownFlushTimeout = 5 * time.Second
)

// TraceStore persists a batch of decoded records. The repo implements it; the
// consumer depends on the interface so batching can be tested without a database.
type TraceStore interface {
	Insert(ctx context.Context, rows []TraceRow) error
}

// integrations maps a deployment to the integration that owns it.
// IntegrationResolver implements it.
type integrations interface {
	Resolve(ctx context.Context, deploymentID string) (string, bool, error)
}

// pricer prices one model call. cost.Refresher implements it, and the indirection
// is what lets a test fix a price instead of standing up a rate card.
type pricer interface {
	Price(call cost.Call) cost.Priced
}

// TraceConsumer subscribes to TraceSubject and writes what arrives in batches.
//
// Batching is the difference from LogConsumer, and it is warranted by volume
// rather than taste: one request through a ten-block flow emits a couple of dozen
// trace records where the same request produces one or two log lines, so the
// per-record round trip that suits the log path would spend most of its time in
// protocol overhead here.
type TraceConsumer struct {
	store        TraceStore
	integrations integrations
	prices       pricer

	dropped atomic.Int64

	warnMu   sync.Mutex
	nextWarn time.Time
}

// NewTraceConsumer returns a consumer that resolves and prices each record before
// handing batches of them to store.
func NewTraceConsumer(store TraceStore, resolver integrations, prices pricer) *TraceConsumer {
	return &TraceConsumer{store: store, integrations: resolver, prices: prices}
}

// Dropped is the running total of records shed because the writer could not keep
// up. It is the number the overflow warning names, exposed so a caller can report
// it without parsing logs.
func (c *TraceConsumer) Dropped() int64 {
	return c.dropped.Load()
}

// Start joins the TraceSubject queue group and writes until the returned
// Subscription is closed (or ctx is cancelled).
//
// One writer, because one is enough and it keeps the batching to a single
// accumulator. It is not a correctness constraint: FoldTraces emits its deltas in
// trace-id order, so any two writers — in this process or in another replica —
// take their row locks in the same order and cannot deadlock on each other.
func (c *TraceConsumer) Start(ctx context.Context, conn *nats.Conn) (*Subscription, error) {
	subCtx, cancel := context.WithCancel(ctx)

	work := make(chan *nats.Msg, traceBuffer)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.write(subCtx, work)
	}()

	sub, err := conn.QueueSubscribe(TraceSubject, traceQueueGroup, func(m *nats.Msg) {
		c.offer(work, m)
	})
	if err != nil {
		cancel()
		wg.Wait()
		return nil, fmt.Errorf("ingest: subscribe %q: %w", TraceSubject, err)
	}
	return &Subscription{sub: sub, cancel: cancel, wg: &wg}, nil
}

// offer hands a delivered record to the writer, shedding it if there is no room.
//
// It never blocks. Blocking here would not stall the other subscriptions — the
// client runs a delivery goroutine per subscription — but it would back records
// up in this one's pending queue until the client hit its own limit and dropped
// them as a slow consumer. That is the same loss, arriving as a client-side
// error with no count of what was lost or which app lost it. Shedding here loses
// the same records and keeps the accounting.
func (c *TraceConsumer) offer(work chan<- *nats.Msg, m *nats.Msg) {
	select {
	case work <- m:
	default:
		c.overflow()
	}
}

// overflow counts a shed record and warns about it at most occasionally, naming
// the running total so one line says how bad it has got rather than how recently.
func (c *TraceConsumer) overflow() {
	total := c.dropped.Add(1)

	c.warnMu.Lock()
	defer c.warnMu.Unlock()
	now := time.Now()
	if now.Before(c.nextWarn) {
		return
	}
	c.nextWarn = now.Add(dropWarnInterval)
	slog.Warn("ingest: dropping trace records, the writer is behind", "dropped", total)
}

// write accumulates rows and flushes them on size or on age, whichever comes
// first, until ctx is done.
//
// Cancellation is checked in every branch rather than left to the ctx.Done() case
// alone, because select does not rank its cases: with a record ready and the
// context already finished, either is a legal choice. Taking the record would
// write it — and the batch it joined — under a context that cannot succeed, which
// is the loss drain exists to prevent. Whichever branch notices first hands over
// to drain and its live context instead.
func (c *TraceConsumer) write(ctx context.Context, in <-chan *nats.Msg) {
	batch := newTraceBatch(c.store)
	defer batch.stop()

	for {
		select {
		case <-ctx.Done():
			c.drain(nil, in, batch)
			return
		case m := <-in:
			if ctx.Err() != nil {
				c.drain(m, in, batch)
				return
			}
			c.take(ctx, m, batch)
		case <-batch.due():
			if ctx.Err() != nil {
				c.drain(nil, in, batch)
				return
			}
			batch.flush(ctx)
		}
	}
}

// drain writes what is already in hand before shutting down, starting with first
// when the loop had already taken a record off the channel.
//
// Delivery has stopped by the time this runs, but records taken off the wire and
// not yet written are lost if they are abandoned — and NATS will not send them
// again. Since the context that got us here is already cancelled, the final write
// gets one of its own, bounded so a stuck database cannot hold the process open.
func (c *TraceConsumer) drain(first *nats.Msg, in <-chan *nats.Msg, batch *traceBatch) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownFlushTimeout)
	defer cancel()

	if first != nil {
		c.take(ctx, first, batch)
	}
	for {
		select {
		case m := <-in:
			c.take(ctx, m, batch)
		default:
			batch.flush(ctx)
			return
		}
	}
}

// take decodes one delivered message into the batch, dropping what cannot be read.
func (c *TraceConsumer) take(ctx context.Context, m *nats.Msg, batch *traceBatch) {
	if row, ok := c.row(ctx, m); ok {
		batch.add(ctx, row)
	}
}

// row turns one delivered message into a row to store.
//
// Only an undecodable record is dropped. A record whose integration could not be
// resolved is stored without one: the trace still happened, integration_id is
// nullable for exactly this reason, and discarding the record would lose the
// evidence of whatever caused the lookup to fail.
func (c *TraceConsumer) row(ctx context.Context, m *nats.Msg) (TraceRow, bool) {
	record, err := parseTraceRecord(m.Data)
	if err != nil {
		slog.Warn("ingest: drop undecodable trace record", "err", err)
		return TraceRow{}, false
	}

	row := TraceRow{Record: record}
	if integrationID, found, err := c.integrations.Resolve(ctx, record.DeploymentID); err != nil {
		slog.Warn("ingest: resolve integration", "deployment", record.DeploymentID, "err", err)
	} else if found {
		row.IntegrationID = integrationID
	}

	// Priced at ingest, against the card in force now, and stored frozen: a rate
	// that changes next month must not silently restate what last month cost.
	if call, isModelCall := record.ModelCall(); isModelCall {
		row.Priced = c.prices.Price(call)
	}
	return row, true
}

// traceBatch is the rows waiting to be written and the deadline they are waiting
// under. It exists to keep the timer's arm/stop/drain bookkeeping in one place
// rather than spread through the select loop.
type traceBatch struct {
	store TraceStore
	rows  []TraceRow

	deadline *time.Timer
	armed    bool
}

func newTraceBatch(store TraceStore) *traceBatch {
	// Created stopped: a batch holding nothing is not waiting for anything.
	deadline := time.NewTimer(traceBatchWait)
	deadline.Stop()
	return &traceBatch{store: store, rows: make([]TraceRow, 0, traceBatchSize), deadline: deadline}
}

// due is the channel that fires when the batch has waited long enough. A batch
// holding nothing has no deadline, so an idle consumer wakes for nothing.
func (b *traceBatch) due() <-chan time.Time {
	return b.deadline.C
}

// add appends a row, starting the clock on the batch's first one and flushing
// once it is full.
func (b *traceBatch) add(ctx context.Context, row TraceRow) {
	b.rows = append(b.rows, row)
	if len(b.rows) >= traceBatchSize {
		b.flush(ctx)
		return
	}
	if !b.armed {
		b.deadline.Reset(traceBatchWait)
		b.armed = true
	}
}

// flush writes the batch and clears it. A failed write is logged and the rows
// dropped, matching the at-most-once delivery the runtime ships them with: there
// is nothing to retry against, and holding them would only make the next failure
// bigger.
func (b *traceBatch) flush(ctx context.Context) {
	b.disarm()
	if len(b.rows) == 0 {
		return
	}
	if err := b.store.Insert(ctx, b.rows); err != nil {
		slog.Error("ingest: store trace records", "records", len(b.rows), "err", err)
	}
	b.rows = b.rows[:0]
}

// disarm cancels the deadline, so a batch flushed for being full does not also
// wake the loop for having aged.
//
// No draining of the timer channel: since Go 1.23 a stopped or reset timer is
// guaranteed not to deliver a stale value afterwards, and this module builds at
// 1.25. The older Stop-then-drain dance would be harmless here but would imply a
// hazard that no longer exists.
func (b *traceBatch) disarm() {
	b.deadline.Stop()
	b.armed = false
}

// stop releases the timer.
func (b *traceBatch) stop() {
	b.deadline.Stop()
}
