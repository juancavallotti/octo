package ingest

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeFolder is the folder's contract without Redis: it holds what it is given
// and hands it back when told. What it pins here is the consumer's half of the
// bargain — that a record given to Append is not also stored, and that what comes
// back is.
type fakeFolder struct {
	held    []TraceRow
	closed  []TraceRow
	appends int
	sweeps  int
	err     error
}

func (f *fakeFolder) Append(_ context.Context, r TraceRow, _ time.Time) ([]TraceRow, error) {
	f.appends++
	if f.err != nil {
		return nil, f.err
	}
	f.held = append(f.held, r)
	out := f.closed
	f.closed = nil
	return out, nil
}

func (f *fakeFolder) Expire(_ context.Context, _ time.Time, _ int) ([]TraceRow, error) {
	f.sweeps++
	if f.err != nil {
		return nil, f.err
	}
	out := f.held
	f.held = nil
	return out, nil
}

func folded(kind string) TraceRow {
	return TraceRow{Record: TraceRecord{TraceID: "tr-1", Kind: kind, Time: time.Now()}}
}

// The sharp edge of the contract: a record handed to the folder must not also go
// into the batch, or every folded run would be stored twice — once per frame and
// once merged.
func TestFoldableRecordsAreHeldRatherThanStored(t *testing.T) {
	folder := &fakeFolder{}
	c := NewTraceConsumer(newTraceStore(), &fixedIntegrations{}, testCard(), folder)
	batch := newTraceBatch(c.store)
	defer batch.stop()

	for _, r := range c.fold(context.Background(), folded(KindBlockPostInvoke)) {
		batch.add(context.Background(), r)
	}

	if folder.appends != 1 {
		t.Errorf("appends = %d, want the record handed to the folder", folder.appends)
	}
	if len(batch.rows) != 0 {
		t.Errorf("batched %d rows, want none while the run is open", len(batch.rows))
	}
}

// Everything that is not a block record goes straight through. Folding it could
// only add a round trip and a delay to something that was already one row.
func TestNonBlockRecordsSkipTheFolder(t *testing.T) {
	folder := &fakeFolder{}
	c := NewTraceConsumer(newTraceStore(), &fixedIntegrations{}, testCard(), folder)

	for _, kind := range []string{KindFlowStarted, KindLLMTurn, KindSourceReceive, KindTraceDropped} {
		out := c.fold(context.Background(), folded(kind))
		if len(out) != 1 {
			t.Errorf("%s produced %d rows, want it stored as it stands", kind, len(out))
		}
	}
	if folder.appends != 0 {
		t.Errorf("appends = %d, want the folder untouched", folder.appends)
	}
}

// Redis being unreachable should cost the folding, not the trace.
func TestAFolderFailureStoresTheRecordAnyway(t *testing.T) {
	folder := &fakeFolder{err: context.DeadlineExceeded}
	c := NewTraceConsumer(newTraceStore(), &fixedIntegrations{}, testCard(), folder)

	out := c.fold(context.Background(), folded(KindBlockPostInvoke))

	if len(out) != 1 {
		t.Fatalf("got %d rows, want the record stored despite the folder failing", len(out))
	}
}

// The sweep is what turns a finished run into a row, and it has to reach the same
// batch every other record does.
func TestSweepBatchesWhatTheFolderReleases(t *testing.T) {
	folder := &fakeFolder{}
	c := NewTraceConsumer(newTraceStore(), &fixedIntegrations{}, testCard(), folder)
	batch := newTraceBatch(c.store)
	defer batch.stop()

	c.fold(context.Background(), folded(KindBlockPostInvoke))
	c.fold(context.Background(), folded(KindBlockPreInvoke))
	c.sweep(context.Background(), batch)

	if len(batch.rows) != 2 {
		t.Errorf("batched %d rows after the sweep, want both held records", len(batch.rows))
	}
}

// A consumer built without a folder behaves exactly as it did before one existed.
// That is what makes the folder an addition rather than a rewrite, and it is the
// configuration every other test in this package runs under.
func TestNoFolderStoresEveryRecordDirectly(t *testing.T) {
	c := NewTraceConsumer(newTraceStore(), &fixedIntegrations{}, testCard(), nil)

	out := c.fold(context.Background(), folded(KindBlockPostInvoke))

	if len(out) != 1 {
		t.Errorf("got %d rows, want the record stored as it stands", len(out))
	}
}

// timedFolder holds records and releases them once a deadline has passed, which
// is the fold store's contract without Redis in the way.
type timedFolder struct {
	mu       sync.Mutex
	held     []TraceRow
	dueAfter time.Time
}

func (f *timedFolder) Append(_ context.Context, r TraceRow, _ time.Time) ([]TraceRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held = append(f.held, r)
	return nil, nil
}

func (f *timedFolder) Expire(_ context.Context, now time.Time, _ int) ([]TraceRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if now.Before(f.dueAfter) {
		return nil, nil
	}
	out := f.held
	f.held = nil
	return out, nil
}

// The bug this test exists for: a consumer whose records are ALL foldable adds no
// rows, so the batch's deadline — which is armed by adding a row — never fires.
// Riding that deadline meant the sweep never ran and the held runs sat in Redis
// until their TTL deleted them, unwritten. A trace made only of block records is
// not exotic; it is what the middle of any streamed answer looks like.
func TestARunOfOnlyFoldableRecordsIsStillSwept(t *testing.T) {
	url := runServer(t)
	pub := connect(t, url)
	conn := connect(t, url)

	// Due immediately, so the first sweep tick releases what was held.
	folder := &timedFolder{dueAfter: time.Now().Add(-time.Second)}
	store := newTraceStore()
	c := NewTraceConsumer(store, resolvesTo(testIntegration), testCard(), folder)

	sub, err := c.Start(context.Background(), conn)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if err := conn.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Nothing but block records: every one of them is held, and nothing else ever
	// arrives to arm a batch.
	for seq := 1; seq <= 3; seq++ {
		if err := pub.Publish(TraceSubject, traceMsg(seq, KindBlockPostInvoke).Data); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	batches := store.awaitBatches(t, 1)
	if len(batches) == 0 || len(batches[0]) == 0 {
		t.Fatal("nothing reached the store: the sweep never ran")
	}
	for _, row := range batches[0] {
		if row.Record.Kind != KindBlockPostInvoke {
			t.Errorf("kind = %q, want the held block records", row.Record.Kind)
		}
	}
}

// countingFolder releases a full pop every time, up to a total, so a sweep that
// stopped after one pop would leave the rest behind.
type countingFolder struct {
	mu        sync.Mutex
	remaining int
	pops      int
}

func (f *countingFolder) Append(context.Context, TraceRow, time.Time) ([]TraceRow, error) {
	return nil, nil
}

func (f *countingFolder) Expire(_ context.Context, _ time.Time, limit int) ([]TraceRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pops++
	n := min(limit, f.remaining)
	f.remaining -= n
	out := make([]TraceRow, n)
	for i := range out {
		out[i] = folded(KindBlockPostInvoke)
	}
	return out, nil
}

// A full pop means there was more due than the script would take at once.
// Waiting a whole interval for the rest would let a backlog grow faster than it
// drains.
func TestSweepKeepsPoppingWhileEachPopComesBackFull(t *testing.T) {
	folder := &countingFolder{remaining: expireLimit*2 + 5}
	c := NewTraceConsumer(newTraceStore(), &fixedIntegrations{}, testCard(), folder)
	batch := newTraceBatch(c.store)
	defer batch.stop()

	c.sweep(context.Background(), batch)

	folder.mu.Lock()
	defer folder.mu.Unlock()
	if folder.remaining != 0 {
		t.Errorf("%d runs left behind, want the backlog drained", folder.remaining)
	}
	// Three pops: two full, then a short one that says there is no more.
	if folder.pops != 3 {
		t.Errorf("pops = %d, want 3", folder.pops)
	}
}

// The backlog must not hold the writer while records are still arriving on the
// channel, so one sweep is bounded however much is due.
func TestSweepStopsAfterItsPassLimit(t *testing.T) {
	folder := &countingFolder{remaining: expireLimit * (expirePasses + 5)}
	c := NewTraceConsumer(newTraceStore(), &fixedIntegrations{}, testCard(), folder)
	batch := newTraceBatch(c.store)
	defer batch.stop()

	c.sweep(context.Background(), batch)

	folder.mu.Lock()
	defer folder.mu.Unlock()
	if folder.pops != expirePasses {
		t.Errorf("pops = %d, want it bounded at %d", folder.pops, expirePasses)
	}
}
