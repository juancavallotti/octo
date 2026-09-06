package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/juancavallotti/octo/observability/internal/cost"
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

	// shutdownFlushTimeout bounds the last write on the way out, for both
	// consumers. Records held at that point are already off the wire and will not
	// be redelivered, so they are worth waiting for — but not worth hanging a
	// shutdown over.
	shutdownFlushTimeout = 5 * time.Second

	// expireLimit is how many finished runs one pop collects.
	//
	// It bounds the script rather than the sweep: since Redis runs a script with
	// nothing interleaved, an unbounded pop would stall every other client for as
	// long as it took. A backlog is worked through by popping again — see sweep —
	// rather than by one long script.
	expireLimit = 200

	// expirePasses bounds one sweep, so a backlog cannot hold the writer
	// indefinitely and starve the records still arriving on the channel. What is
	// left waits for the next tick, which is at most sweepInterval away.
	expirePasses = 10

	// sweepInterval is how often finished runs are collected.
	//
	// It has a timer of its own rather than riding the batch deadline, and that is
	// a correctness matter rather than a tidiness one. The batch's timer is armed
	// by adding a row — and a consumer whose records are all foldable adds none,
	// because they are all being held. The deadline would never fire, the sweep
	// would never run, and the runs would sit in Redis until their TTL deleted
	// them unwritten. A trace made entirely of block records is not exotic: it is
	// what the middle of any streamed answer looks like.
	//
	// Half a second against a one-second fold window, so a finished run is written
	// within about a window and a half of going quiet. An idle tick is one
	// ZRANGEBYSCORE against an empty set.
	sweepInterval = 500 * time.Millisecond
)

// Folder holds runs of near-identical records and hands back the one record that
// stands for each finished run. fold.Store implements it.
//
// The contract is the sharp edge here: a record given to Append is NOT stored by
// the caller. It reaches the database only through what Append or Expire returns —
// folded into its run's record, or handed back unchanged when the run was too
// short to be worth folding.
// Stated in terms of TraceRow rather than fold's own alias for it, which is what
// keeps the dependency one-way: fold reads this package's type, and this package
// knows only the shape of something that folds.
type Folder interface {
	Append(ctx context.Context, r TraceRow, now time.Time) ([]TraceRow, error)
	Expire(ctx context.Context, now time.Time, limit int) ([]TraceRow, error)
}

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
	shedder

	store        TraceStore
	integrations integrations
	prices       pricer
	folder       Folder
}

// NewTraceConsumer returns a consumer that resolves and prices each record before
// handing batches of them to store, folding runs of block records through folder.
func NewTraceConsumer(store TraceStore, resolver integrations, prices pricer, folder Folder) *TraceConsumer {
	return &TraceConsumer{
		shedder:      shedder{what: "trace"},
		store:        store,
		integrations: resolver,
		prices:       prices,
		folder:       folder,
	}
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

	// A nil channel blocks forever in a select, which is exactly what a consumer
	// with no folder wants: no ticker, and no case that can fire.
	var sweeps <-chan time.Time
	if c.folder != nil {
		ticker := time.NewTicker(sweepInterval)
		defer ticker.Stop()
		sweeps = ticker.C
	}

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
		case <-sweeps:
			if ctx.Err() != nil {
				c.drain(nil, in, batch)
				return
			}
			c.sweep(ctx, batch)
			// Flushed here as well as on the batch's own deadline: a sweep may be the
			// only thing that produced a row, and adding one arms a deadline that is
			// then the only reason the batch would ever be written.
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
			// One last sweep before the flush. Runs still open here belong to
			// whichever replica sweeps next — the state is in Redis, not in this
			// process — but a single-replica install has no next replica, so
			// collecting what is already due is the difference between those records
			// being written and being dropped on every restart.
			c.sweep(ctx, batch)
			batch.flush(ctx)
			return
		}
	}
}

// take decodes one delivered message into the batch, dropping what cannot be read.
func (c *TraceConsumer) take(ctx context.Context, m *nats.Msg, batch *traceBatch) {
	row, ok := c.row(ctx, m)
	if !ok {
		return
	}
	for _, r := range c.fold(ctx, row) {
		batch.add(ctx, r)
	}
}

// fold routes a record through the folder, returning whatever is ready to store.
//
// Only block records go through it, and that is a deliberate limit rather than a
// first step. They are where the volume is — a streaming block emits a pre-invoke
// and a post-invoke per frame, which for an agent is per token — and everything
// else is one record per event, so folding it could only add a round trip and a
// delay to something that was already one row.
//
// The delay is the cost worth naming: a block record is held for as long as the
// fold window before it is stored, because a run cannot be recognised from its
// first record. Traces are read after the fact, so that is affordable; it would
// not be if anything watched them live.
//
// A folder error stores the record as it stands rather than dropping it. Redis
// being unreachable should cost the folding, not the trace.
func (c *TraceConsumer) fold(ctx context.Context, row TraceRow) []TraceRow {
	if c.folder == nil || !foldable(row.Record.Kind) {
		return []TraceRow{row}
	}
	out, err := c.folder.Append(ctx, row, time.Now())
	if err != nil {
		slog.Warn("ingest: fold trace record", "kind", row.Record.Kind, "err", err)
		return []TraceRow{row}
	}
	return out
}

// foldable reports whether a kind is worth holding. See fold above for why the
// answer is the two block kinds and nothing else.
func foldable(kind string) bool {
	return kind == KindBlockPreInvoke || kind == KindBlockPostInvoke
}

// sweep collects the runs that have gone quiet and adds them to the batch.
//
// It pops repeatedly while each pop comes back full, because a full pop means
// there was more due than the script would take in one go — and waiting a whole
// interval to collect the rest would let a backlog grow faster than it drains.
// Bounded by expirePasses so a backlog cannot hold the writer while records are
// still arriving.
func (c *TraceConsumer) sweep(ctx context.Context, batch *traceBatch) {
	if c.folder == nil {
		return
	}
	for range expirePasses {
		done, err := c.folder.Expire(ctx, time.Now(), expireLimit)
		if err != nil {
			slog.Warn("ingest: sweep folded traces", "err", err)
			return
		}
		for _, r := range done {
			batch.add(ctx, r)
		}
		if len(done) < expireLimit {
			return
		}
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
