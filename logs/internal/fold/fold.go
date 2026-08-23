// Package fold collapses a run of near-identical trace records into one.
//
// The records that make this worth doing come from streaming. An `sse-event`
// block is an ordinary block, so the engine emits a block.pre-invoke and a
// block.post-invoke for every frame it writes — which, for an agent streaming an
// answer, is every token. One conversation with the platform agent produced a
// single trace of 26,508 records; across the whole traces table those records
// were 93% of the bytes and none of the insight. Nobody has ever read the
// waterfall for a streamed answer, because it is thirty thousand spans of one
// word each.
//
// So a run becomes one record: the first record's identity, the last record's
// end, a count, and — where the bodies allow it — the text of the whole run
// concatenated back into something a person can read. That last part is the
// reason to fold rather than to sample. Losing 29,999 rows saves space; getting
// the streamed answer back as one block of prose is what makes the trace useful.
//
// This file is the arithmetic and nothing else: no Redis, no clock, no I/O. What
// decides when a run has ended lives in the store.
package fold

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/juancavallotti/octo/logs/internal/ingest"
)

// Attribute the folded record carries, and the keys under it.
//
// It goes in attrs rather than in a column because it describes how the row was
// made rather than what was traced, and because attrs is already the place a
// reader looks for that: the runtime writes bodyBytes there when it truncates a
// payload, for the same reason.
const (
	AttrFolded = "folded"

	attrCount    = "count"
	attrFirstSeq = "firstSeq"
	attrLastSeq  = "lastSeq"
	attrBodies   = "bodies"
)

// What happened to the bodies of a folded run, written under attrs.folded.bodies.
//
// Only the lossy case is recorded. A merged body needs no annotation — it is the
// text, and the count beside it says how many records it came from — but a body
// that is only the first of a run is a row that would otherwise look complete
// while being a thousandth of what happened.
const BodiesFirst = "first"

// Record is one trace record as this package needs it: what arrived, plus the
// stream identity ingest resolved.
//
// It is ingest.TraceRow rather than a type of its own because a folded run has to
// go back into the same batch the unfolded records would have — the store, the
// summary fold and the columns downstream all take that type, and a parallel one
// would only need converting at both ends.
type Record = ingest.TraceRow

// Key identifies the run a record belongs to.
//
// Kind is part of it, and that is the part worth explaining: pre-invoke and
// post-invoke alternate, so a rule that ended a run whenever the next record's
// kind differed would end every run at length one and fold nothing at all. Two
// runs are open for the same block instead — one per kind — and both collapse.
//
// BlockType is in the key without being able to vary within a path, which makes
// it redundant. It is here anyway so the key is self-describing: a fold read back
// out of the store says what kind of block it came from without a join.
type Key struct {
	TraceID   string
	Kind      string
	Path      string
	BlockType string
}

// KeyOf returns the key for a record.
func KeyOf(r Record) Key {
	return Key{
		TraceID:   r.Record.TraceID,
		Kind:      r.Record.Kind,
		Path:      r.Record.Path,
		BlockType: r.Record.BlockType,
	}
}

// Open is a run being accumulated.
//
// The first record is kept whole because the folded row is mostly it: the same
// seq, event id, correlation id, flow and path, so the waterfall's (eventId,
// path) nesting key still resolves and a folded span sits exactly where the
// unfolded ones did. Everything else here is what the rest of the run
// contributed.
type Open struct {
	Key   Key
	First Record

	Count int

	// LastSeq and LastEnd are where the run finished. LastEnd is the last
	// record's timestamp, which is its *end*: the runtime stamps a record when the
	// traced thing completed.
	LastSeq int64
	LastEnd time.Time

	// Shape is the first body's non-string fields, canonicalised. A record whose
	// shape differs is not part of this run — see SameShape.
	Shape string

	// Chunks is the run's text by seq. A map rather than a growing string because
	// records arrive out of order — across replicas they are not even ordered
	// per-consumer — and joining them in arrival order would garble the answer
	// this fold exists to make readable.
	Chunks map[int64]string
	// Bytes is what Chunks holds, tracked rather than recomputed so the cap can be
	// checked without walking the map on every record.
	Bytes int
	// Truncated is set when the cap stopped a chunk being kept.
	Truncated bool
	// Mergeable says the first record had a payload field, which is what gives the
	// merged text somewhere to go. It is decided by the first record and does not
	// change: every later record shares this one's shape, and the shape is what
	// determines whether a payload field is there at all.
	//
	// False is not a failure. The run still folds — one row with a count is where
	// nearly all of the space saving is — and the record says so.
	Mergeable bool

	// Err is the first non-empty error in the run, and Dropped the OR of every
	// record's flag. Both are properties of the run rather than of any record in
	// it: a run where one frame failed is a run that failed.
	Err     string
	Dropped bool
}

// Start begins a run at r.
func Start(r Record) *Open {
	shape, text, mergeable := split(r.Record.Body)
	o := &Open{
		Key:       KeyOf(r),
		First:     r,
		Count:     1,
		LastSeq:   r.Record.Seq,
		LastEnd:   r.Record.Time,
		Shape:     shape,
		Chunks:    map[int64]string{},
		Mergeable: mergeable,
		Err:       r.Record.Err,
		Dropped:   r.Record.Dropped || r.Record.Truncated,
	}
	if mergeable {
		o.Chunks[r.Record.Seq] = text
		o.Bytes = len(text)
	}
	return o
}

// SameShape reports whether r belongs to the run o is accumulating.
//
// The shape is every non-string field of the body, which for a streamed frame is
// what says *what* is streaming: {"text": "...", "type": "thinking", "index": 0,
// "iteration": 4}. The text differs every frame and is the thing being
// concatenated; type, index and iteration are constant for as long as one thing
// is being streamed and change when the next begins. Comparing them is what keeps
// a run of thinking from merging into the answer that follows it.
func (o *Open) SameShape(r Record) bool {
	shape, _, _ := split(r.Record.Body)
	return shape == o.Shape
}

// Absorb adds r to the run. maxBytes caps the merged text; past it, chunks are
// dropped and the fold is marked truncated.
func (o *Open) Absorb(r Record, maxBytes int) {
	o.Count++
	if r.Record.Seq > o.LastSeq {
		o.LastSeq = r.Record.Seq
	}
	if r.Record.Time.After(o.LastEnd) {
		o.LastEnd = r.Record.Time
	}
	if o.Err == "" {
		o.Err = r.Record.Err
	}
	o.Dropped = o.Dropped || r.Record.Dropped || r.Record.Truncated

	if !o.Mergeable {
		return
	}
	// A shape-matching record either carries a payload field or has none; both are
	// ordinary. One with none contributes an empty string, which is exactly what it
	// contributed to the stream.
	_, text, _ := split(r.Record.Body)
	if text == "" {
		return
	}
	if o.Bytes+len(text) > maxBytes {
		o.Truncated = true
		return
	}
	o.Chunks[r.Record.Seq] = text
	o.Bytes += len(text)
}

// Close turns the run into the single record that stands for it.
//
// Below min the run is not worth rewriting: the attrs a fold adds cost more than
// two rows save, and a reader looking at a two-record span learns nothing from
// being told it is two records. Those come back as the first record unchanged,
// which is only correct because a short run's later records are handed back
// separately by the store.
func (o *Open) Close(min int) (Record, bool) {
	if o.Count < min {
		return o.First, false
	}

	out := o.First
	// The interval, preserving the rule the whole stack reads records by:
	// [ts - durationNs, ts]. The run began where the first record began — its
	// stamp minus its own duration — and ended at the last record's stamp.
	out.Record.Time = o.LastEnd
	if began := o.First.Record.Time.Add(-time.Duration(o.First.Record.DurationNs)); o.LastEnd.After(began) {
		out.Record.DurationNs = o.LastEnd.Sub(began).Nanoseconds()
	}
	out.Record.Err = o.Err
	out.Record.Dropped = o.Dropped
	out.Record.Truncated = o.Truncated || o.First.Record.Truncated

	if o.Mergeable {
		out.Record.Body = merged(o.First.Record.Body, o.join())
	}
	out.Record.Attrs = withFolded(o.First.Record.Attrs, o.Count, o.First.Record.Seq, o.LastSeq, o.Mergeable)
	return out, true
}

// join concatenates the run's chunks in sequence order.
func (o *Open) join() string {
	seqs := make([]int64, 0, len(o.Chunks))
	for seq := range o.Chunks {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })

	out := make([]byte, 0, o.Bytes)
	for _, seq := range seqs {
		out = append(out, o.Chunks[seq]...)
	}
	return string(out)
}

// Field names a streamed frame puts its payload in.
//
// This list is the one piece of convention in the package, and it is worth being
// plain about why it is a list rather than a rule. The obvious rule — "the string
// fields are the text, the rest is the shape" — does not survive contact with the
// data: an sse-event frame is {"text": "...", "type": "thinking", "index": 0},
// and `type` is a string that must NOT be concatenated. It is precisely the field
// that says a run of thinking has ended and an answer has begun, and merging it
// would fold the two together.
//
// The alternative rule — "the field that varies is the text" — is correct but
// cannot be evaluated on one record, and this decision has to be made per record:
// the script that accumulates a run compares shapes as opaque strings, and giving
// it a two-record handshake to perform would put real logic in Lua for a case that
// a name covers.
//
// So: merging is best-effort and keyed on the names streaming actually uses.
// **Folding does not depend on it.** A body this list does not recognise still
// collapses its run to one row with a count — which is where nearly all of the
// space goes — and says so with attrs.folded.bodies.
var mergeFields = map[string]bool{
	"text":    true,
	"delta":   true,
	"content": true,
	"chunk":   true,
}

// split separates a body into the part that identifies the run and the part that
// accumulates.
//
// The shape is every field except the merge field, key and raw value both, in key
// order — so {"index":0} and {"index":1} are different shapes, and {"a":0} is not
// the same shape as {"b":0}. The text is the merge field's value.
//
// The third return says whether the body can take part in a merge at all: it needs
// to be a JSON object with a recognised string payload field. Without one there is
// nothing to concatenate, and a folded record whose body was the first frame's
// while looking like the whole run's would be worse than one that admits it.
func split(body json.RawMessage) (shape, text string, mergeable bool) {
	if len(body) == 0 {
		return "", "", false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return "", "", false
	}

	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var shapeBuf []byte
	for _, k := range keys {
		if mergeFields[k] {
			var s string
			if json.Unmarshal(fields[k], &s) == nil {
				mergeable = true
				text = s
				continue
			}
			// A merge-named field holding something other than a string is shape like
			// anything else. Nothing streams that way, but assuming it does not would
			// mean silently dropping the field from the shape.
		}
		shapeBuf = append(shapeBuf, k...)
		shapeBuf = append(shapeBuf, '=')
		shapeBuf = append(shapeBuf, fields[k]...)
		shapeBuf = append(shapeBuf, ';')
	}
	return string(shapeBuf), text, mergeable
}

// merged rebuilds the first body with its payload field replaced by the run's
// joined text. Every other field is the first record's, which is also every other
// record's — that is what made them one run.
func merged(body json.RawMessage, text string) json.RawMessage {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return body
	}

	encoded, err := json.Marshal(text)
	if err != nil {
		return body
	}
	for k := range fields {
		if !mergeFields[k] {
			continue
		}
		var s string
		if json.Unmarshal(fields[k], &s) != nil {
			continue
		}
		fields[k] = encoded
		break
	}

	out, err := json.Marshal(fields)
	if err != nil {
		return body
	}
	return out
}

// withFolded writes the fold's own accounting into a record's attributes,
// preserving whatever the runtime put there.
func withFolded(attrs json.RawMessage, count int, firstSeq, lastSeq int64, mergeable bool) json.RawMessage {
	fields := map[string]json.RawMessage{}
	if len(attrs) > 0 {
		// An unreadable attrs object is replaced rather than preserved: it could not
		// be read downstream either, and losing it costs less than losing the count.
		_ = json.Unmarshal(attrs, &fields)
	}
	// A record whose attrs were absent comes back from the store as the four bytes
	// `null`, which unmarshals into a map by setting it to nil rather than by
	// failing — so the check above passes and the assignment below panics. Absent
	// and empty mean the same thing here, and this is where they are made to.
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}

	folded := map[string]any{
		attrCount:    count,
		attrFirstSeq: firstSeq,
		attrLastSeq:  lastSeq,
	}
	if !mergeable {
		folded[attrBodies] = BodiesFirst
	}

	encoded, err := json.Marshal(folded)
	if err != nil {
		return attrs
	}
	fields[AttrFolded] = encoded

	out, err := json.Marshal(fields)
	if err != nil {
		return attrs
	}
	return out
}

// String renders a key as the store's key suffix. Trace id first so every fold
// for one trace shares a prefix, which keeps a trace's keys together in whatever
// tooling somebody eventually points at them.
func (k Key) String() string {
	return k.TraceID + "|" + k.Kind + "|" + k.Path + "|" + k.BlockType
}
