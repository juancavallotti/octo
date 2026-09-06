package repo

import (
	"testing"

	"github.com/juancavallotti/octo/observability/internal/fold"
	"github.com/juancavallotti/octo/observability/internal/ingest"
)

// The property that lets the aggregator fold at all: a summary is built from the
// records it is given, so a folded run and the run it stands for have to produce
// the same interval.
//
// If they did not, folding would silently move when a trace started or ended, and
// the trace list — which sorts and filters on exactly those columns — would be
// answering with a different set of traces than before.
func TestAFoldedRunSummarizesToTheSameInterval(t *testing.T) {
	// A run of four frames, the way a streaming block emits them.
	var run []ingest.TraceRow
	for i := range 4 {
		run = append(run, record(int64(i+1), ingest.KindBlockPostInvoke,
			endingAt(100+10*i, 5), inFlow("chat")))
	}

	open := fold.Start(run[0])
	for _, r := range run[1:] {
		open.Absorb(r, 1<<20)
	}
	collapsed, ok := open.Close(4)
	if !ok {
		t.Fatal("want the run folded")
	}

	unfolded := FoldTraces(run)
	folded := FoldTraces([]ingest.TraceRow{collapsed})

	if len(unfolded) != 1 || len(folded) != 1 {
		t.Fatalf("summaries = %d and %d, want one each", len(unfolded), len(folded))
	}
	if !unfolded[0].StartedAt.Equal(folded[0].StartedAt) {
		t.Errorf("startedAt = %v folded, %v unfolded", folded[0].StartedAt, unfolded[0].StartedAt)
	}
	if !unfolded[0].EndedAt.Equal(folded[0].EndedAt) {
		t.Errorf("endedAt = %v folded, %v unfolded", folded[0].EndedAt, unfolded[0].EndedAt)
	}

	// The count is the one column that deliberately differs, and it has to: it is
	// what the detail view says it will show, and after a fold there are fewer rows
	// to show. A summary claiming four while the waterfall drew one would be the
	// panel and the trace disagreeing about the same run.
	if folded[0].Records != 1 {
		t.Errorf("records = %d, want the rows actually stored", folded[0].Records)
	}
	if unfolded[0].Records != 4 {
		t.Errorf("unfolded records = %d, want 4", unfolded[0].Records)
	}
}

// Folding must not change what a trace's root flow or entry point is: those are
// chosen by sequence, and a fold keeps the first record's.
func TestAFoldedRunKeepsTheTracesIdentity(t *testing.T) {
	entry := record(1, ingest.KindSourceReceive, endingAt(0, 0),
		withAttrs(`{"method":"POST","route":"/chat"}`))

	var run []ingest.TraceRow
	for i := range 4 {
		run = append(run, record(int64(i+2), ingest.KindBlockPostInvoke,
			endingAt(10+10*i, 1), inFlow("chat")))
	}

	open := fold.Start(run[0])
	for _, r := range run[1:] {
		open.Absorb(r, 1<<20)
	}
	collapsed, _ := open.Close(4)

	unfolded := FoldTraces(append([]ingest.TraceRow{entry}, run...))
	folded := FoldTraces([]ingest.TraceRow{entry, collapsed})

	if unfolded[0].EntryLabel != folded[0].EntryLabel {
		t.Errorf("entry = %q folded, %q unfolded", folded[0].EntryLabel, unfolded[0].EntryLabel)
	}
	if unfolded[0].RootFlow != folded[0].RootFlow {
		t.Errorf("rootFlow = %q folded, %q unfolded", folded[0].RootFlow, unfolded[0].RootFlow)
	}
	if unfolded[0].RootFlowSeq != folded[0].RootFlowSeq {
		t.Errorf("rootFlowSeq = %d folded, %d unfolded", folded[0].RootFlowSeq, unfolded[0].RootFlowSeq)
	}
}

// A run where one frame failed is a run that failed, and the summary has to say
// so — the trace list's failed filter reads this column.
func TestAFoldedRunCarriesAFailureIntoTheSummary(t *testing.T) {
	var run []ingest.TraceRow
	for i := range 4 {
		r := record(int64(i+1), ingest.KindBlockPostInvoke, endingAt(10+10*i, 1), inFlow("chat"))
		if i == 2 {
			r.Record.Dropped = true
		}
		run = append(run, r)
	}
	run = append(run, record(9, ingest.KindFlowCompleted, endingAt(100, 100), inFlow("chat")))

	open := fold.Start(run[0])
	for _, r := range run[1:4] {
		open.Absorb(r, 1<<20)
	}
	collapsed, _ := open.Close(4)

	unfolded := FoldTraces(run)
	folded := FoldTraces([]ingest.TraceRow{collapsed, run[4]})

	if unfolded[0].Status != folded[0].Status {
		t.Errorf("status = %q folded, %q unfolded", folded[0].Status, unfolded[0].Status)
	}
}
