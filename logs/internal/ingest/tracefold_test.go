package ingest

import (
	"context"
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
