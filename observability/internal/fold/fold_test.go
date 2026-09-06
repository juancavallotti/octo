package fold

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/ingest"
)

var base = time.Date(2026, 8, 23, 3, 0, 0, 0, time.UTC)

// rec builds a record shaped like the ones this package exists for: one frame of
// a streamed answer, as the sse-event block emits it.
func rec(seq int64, at time.Duration, body string) Record {
	return Record{Record: ingest.TraceRecord{
		TraceID:    "tr-1",
		Seq:        seq,
		Kind:       ingest.KindBlockPostInvoke,
		EventID:    "ev-1",
		Flow:       "chat",
		Path:       "chat.dr-octo[events].sse-event",
		BlockType:  "sse-event",
		Time:       base.Add(at),
		DurationNs: int64(100 * time.Microsecond),
		Body:       json.RawMessage(body),
	}}
}

func frame(seq int64, at time.Duration, text string) Record {
	b, _ := json.Marshal(map[string]any{"text": text, "type": "thinking", "index": 0})
	return rec(seq, at, string(b))
}

/** Run every record through a fresh run, as the store does. */
func run(t *testing.T, records ...Record) *Open {
	t.Helper()
	o := Start(records[0])
	for _, r := range records[1:] {
		if !o.SameShape(r) {
			t.Fatalf("seq %d unexpectedly changed shape", r.Record.Seq)
		}
		o.Absorb(r, 1<<20)
	}
	return o
}

func bodyOf(t *testing.T, r Record) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(r.Record.Body, &out); err != nil {
		t.Fatalf("body is not an object: %v", err)
	}
	return out
}

func foldedAttr(t *testing.T, r Record) map[string]any {
	t.Helper()
	var attrs map[string]any
	if err := json.Unmarshal(r.Record.Attrs, &attrs); err != nil {
		t.Fatalf("attrs is not an object: %v", err)
	}
	folded, ok := attrs[AttrFolded].(map[string]any)
	if !ok {
		t.Fatalf("attrs carries no %q: %v", AttrFolded, attrs)
	}
	return folded
}

// The point of the whole package: a streamed answer reads back as one block of
// prose rather than as one word per row.
func TestCloseMergesTheStreamedText(t *testing.T) {
	o := run(t,
		frame(1, 0, "Checking "),
		frame(2, time.Millisecond, "the "),
		frame(3, 2*time.Millisecond, "namespace"),
		frame(4, 3*time.Millisecond, "."),
	)

	out, folded := o.Close(4)
	if !folded {
		t.Fatal("want the run folded")
	}
	if got := bodyOf(t, out)["text"]; got != "Checking the namespace." {
		t.Errorf("text = %q, want the run concatenated", got)
	}
	// The fields that said what was streaming survive: they are what a reader uses
	// to tell thinking from an answer.
	if got := bodyOf(t, out)["type"]; got != "thinking" {
		t.Errorf("type = %v, want it preserved", got)
	}
}

// Records do not arrive in order — across replicas they are not even ordered
// per-consumer — so arrival order must not decide what the text says.
func TestCloseJoinsChunksInSequenceOrder(t *testing.T) {
	o := run(t,
		frame(1, 0, "one "),
		frame(4, 3*time.Millisecond, "four"),
		frame(2, time.Millisecond, "two "),
		frame(3, 2*time.Millisecond, "three "),
	)

	out, _ := o.Close(4)
	if got := bodyOf(t, out)["text"]; got != "one two three four" {
		t.Errorf("text = %q, want it joined by seq rather than by arrival", got)
	}
}

// The identity the waterfall nests on. A folded span has to sit exactly where the
// unfolded ones did, or the trace tree comes apart.
func TestCloseKeepsTheFirstRecordsIdentity(t *testing.T) {
	first := frame(7, 0, "a")
	o := run(t, first, frame(8, time.Millisecond, "b"), frame(9, 2*time.Millisecond, "c"), frame(10, 3*time.Millisecond, "d"))

	out, _ := o.Close(4)
	if out.Record.Seq != first.Record.Seq {
		t.Errorf("seq = %d, want the first record's %d", out.Record.Seq, first.Record.Seq)
	}
	for _, f := range []struct{ name, got, want string }{
		{"eventId", out.Record.EventID, first.Record.EventID},
		{"path", out.Record.Path, first.Record.Path},
		{"flow", out.Record.Flow, first.Record.Flow},
		{"blockType", out.Record.BlockType, first.Record.BlockType},
	} {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q", f.name, f.got, f.want)
		}
	}
}

// The interval rule the whole stack reads records by: [ts - durationNs, ts]. A
// folded span covers the run, so its end is the last record's stamp and its start
// is where the first one started.
func TestCloseSpansTheWholeRun(t *testing.T) {
	o := run(t,
		frame(1, 0, "a"),
		frame(2, time.Second, "b"),
		frame(3, 2*time.Second, "c"),
		frame(4, 3*time.Second, "d"),
	)

	out, _ := o.Close(4)
	if !out.Record.Time.Equal(base.Add(3 * time.Second)) {
		t.Errorf("ts = %v, want the last record's stamp", out.Record.Time)
	}
	began := out.Record.Time.Add(-time.Duration(out.Record.DurationNs))
	wantBegan := base.Add(-100 * time.Microsecond)
	if !began.Equal(wantBegan) {
		t.Errorf("span begins at %v, want %v (the first record's own start)", began, wantBegan)
	}
}

func TestCloseRecordsTheCount(t *testing.T) {
	o := run(t, frame(1, 0, "a"), frame(2, time.Millisecond, "b"),
		frame(3, 2*time.Millisecond, "c"), frame(9, 3*time.Millisecond, "d"))

	out, _ := o.Close(4)
	folded := foldedAttr(t, out)
	if folded["count"] != float64(4) {
		t.Errorf("count = %v, want 4", folded["count"])
	}
	if folded["firstSeq"] != float64(1) || folded["lastSeq"] != float64(9) {
		t.Errorf("seq range = %v..%v, want 1..9", folded["firstSeq"], folded["lastSeq"])
	}
}

// The boundary that keeps an agent's thinking out of its answer. Both are runs of
// {"text": ...}; only the fields beside the text say which is which.
func TestSameShapeSeparatesThinkingFromTheAnswer(t *testing.T) {
	o := Start(frame(1, 0, "thinking about it"))

	answer, _ := json.Marshal(map[string]any{"text": "here it is", "type": "text", "index": 0})
	if o.SameShape(rec(2, time.Millisecond, string(answer))) {
		t.Error("want a different type to end the run")
	}

	more, _ := json.Marshal(map[string]any{"text": " some more", "type": "thinking", "index": 0})
	if !o.SameShape(rec(2, time.Millisecond, string(more))) {
		t.Error("want more of the same thing to extend the run")
	}
}

// A run below the threshold is handed back as its first record, unchanged. The
// store is what returns the rest of it — see decodePending.
func TestCloseLeavesAShortRunAlone(t *testing.T) {
	o := run(t, frame(1, 0, "a"), frame(2, time.Millisecond, "b"))

	out, folded := o.Close(4)
	if folded {
		t.Fatal("want a two-record run left unfolded")
	}
	if string(out.Record.Body) != string(o.First.Record.Body) {
		t.Errorf("body = %s, want the first record's untouched", out.Record.Body)
	}
	if len(out.Record.Attrs) != 0 {
		t.Errorf("attrs = %s, want nothing added to an unfolded record", out.Record.Attrs)
	}
}

// Past the cap the text stops growing and the record says so, using the same flag
// the runtime sets when it drops a payload of its own.
func TestAbsorbCapsTheMergedText(t *testing.T) {
	o := Start(frame(1, 0, "12345"))
	o.Absorb(frame(2, time.Millisecond, "67890"), 8)
	o.Absorb(frame(3, 2*time.Millisecond, "abcde"), 8)
	o.Absorb(frame(4, 3*time.Millisecond, "fghij"), 8)

	out, folded := o.Close(4)
	if !folded {
		t.Fatal("want the run folded")
	}
	if !out.Record.Truncated {
		t.Error("want the record marked truncated")
	}
	if got := bodyOf(t, out)["text"]; got != "12345" {
		t.Errorf("text = %q, want only what fit", got)
	}
	// The count is of records, not of what survived the cap: it is how many things
	// happened, which is true either way.
	if foldedAttr(t, out)["count"] != float64(4) {
		t.Error("want the count to include the records whose text was dropped")
	}
}

// A body with no payload field to merge into still folds — one row with a count
// is where nearly all of the space saving is — and says what its body is.
func TestARunWithNothingToMergeStillFolds(t *testing.T) {
	plain := func(seq int64, at time.Duration) Record {
		return rec(seq, at, `{"type":"ping","index":0}`)
	}
	o := run(t, plain(1, 0), plain(2, time.Millisecond),
		plain(3, 2*time.Millisecond), plain(4, 3*time.Millisecond))

	out, folded := o.Close(4)
	if !folded {
		t.Fatal("want the run folded")
	}
	if foldedAttr(t, out)["bodies"] != BodiesFirst {
		t.Error("want the record to say its body is only the first of the run")
	}
	if foldedAttr(t, out)["count"] != float64(4) {
		t.Error("want the count regardless")
	}
}

// A frame that carried no text is part of the run and adds nothing to it, which
// is exactly what it added to the stream. It must not cost the run its merge.
func TestAnEmptyFrameDoesNotBreakTheMerge(t *testing.T) {
	empty, _ := json.Marshal(map[string]any{"type": "thinking", "index": 0})

	o := Start(frame(1, 0, "a"))
	blank := rec(2, time.Millisecond, string(empty))
	if !o.SameShape(blank) {
		t.Fatal("want a frame with no text to share the run's shape")
	}
	o.Absorb(blank, 1<<20)
	o.Absorb(frame(3, 2*time.Millisecond, "b"), 1<<20)
	o.Absorb(frame(4, 3*time.Millisecond, "c"), 1<<20)

	out, folded := o.Close(4)
	if !folded {
		t.Fatal("want the run folded")
	}
	if got := bodyOf(t, out)["text"]; got != "abc" {
		t.Errorf("text = %q, want the frames that had text, merged", got)
	}
}

// A run where one frame failed is a run that failed, and the first error is the
// one that explains it.
func TestCloseCarriesTheFirstErrorAndAnyDrop(t *testing.T) {
	a := frame(1, 0, "a")
	b := frame(2, time.Millisecond, "b")
	b.Record.Err = "stream closed"
	c := frame(3, 2*time.Millisecond, "c")
	c.Record.Err = "and again"
	d := frame(4, 3*time.Millisecond, "d")
	d.Record.Dropped = true

	o := run(t, a, b, c, d)
	out, _ := o.Close(4)

	if out.Record.Err != "stream closed" {
		t.Errorf("error = %q, want the first one in the run", out.Record.Err)
	}
	if !out.Record.Dropped {
		t.Error("want the run marked dropped")
	}
}

// Whatever the runtime put in attrs is still there afterwards: the fold adds a
// key, it does not take the object over.
func TestCloseKeepsExistingAttributes(t *testing.T) {
	first := frame(1, 0, "a")
	first.Record.Attrs = json.RawMessage(`{"bodyBytes":128}`)
	o := run(t, first, frame(2, time.Millisecond, "b"),
		frame(3, 2*time.Millisecond, "c"), frame(4, 3*time.Millisecond, "d"))

	out, _ := o.Close(4)
	var attrs map[string]any
	if err := json.Unmarshal(out.Record.Attrs, &attrs); err != nil {
		t.Fatalf("attrs: %v", err)
	}
	if attrs["bodyBytes"] != float64(128) {
		t.Errorf("bodyBytes = %v, want it preserved", attrs["bodyBytes"])
	}
	if _, ok := attrs[AttrFolded]; !ok {
		t.Error("want the fold's own accounting alongside it")
	}
}

// Kind is in the key because pre-invoke and post-invoke alternate: a rule that
// ended a run whenever the next record's kind differed would fold nothing at all.
func TestKeySeparatesTheTwoBlockKinds(t *testing.T) {
	pre := frame(1, 0, "a")
	pre.Record.Kind = ingest.KindBlockPreInvoke
	post := frame(2, 0, "a")

	if KeyOf(pre) == KeyOf(post) {
		t.Error("want pre-invoke and post-invoke to be separate runs")
	}
}
