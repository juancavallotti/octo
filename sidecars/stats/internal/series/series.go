// Package series turns a Prometheus scrape into a dictionary of series
// identities plus a flat vector of float64 values, and keeps that dictionary
// stable across scrapes.
//
// Why a dictionary at all. This sidecar captures everything the runtime's
// /metrics serves — the go_* set, the process_* set and every octo_* family —
// which is 60 to 100 series once a couple of flows are running. Written as
// self-describing JSON, one sample of that is 3-6 KiB, so an hour of one-second
// samples is 11-22 MiB for a single pod. The Redis those samples go to is the
// same 256Mi allkeys-lru instance the trace-fold pipeline and the volatile KV
// tier share (helm/values.yaml), so a handful of pods sampling that way would
// evict the folds. Interning each identity once and writing samples as
// [gen, tMs, v0, v1, ...] costs about 700 bytes a sample instead.
//
// What that costs is reading a raw sample with redis-cli: the numbers mean
// nothing without the dictionary. The dictionary is a plain hash stored beside
// the samples under the same pod, and store.Layout says where.
//
// Why generations. The series set is not fixed for the life of a pod: a config
// reload adds a flow, and octo_flow_messages_total gains a label value that has
// never been seen. Rather than rewrite history, the dictionary appends the new
// identities and bumps its generation, and every sample records the generation
// it was encoded against. A reader that holds dict:3 can decode a gen-3 sample
// exactly, and older samples stay readable against the dictionary they were
// written with.
//
// Indices are only ever appended, never reused or renumbered, which is what
// makes a dictionary a superset of every earlier one and lets a reader that
// fetched the newest generation decode older samples too.
package series

import (
	"sort"
	"strings"

	dto "github.com/prometheus/client_model/go"
)

// Kind is the collapse rule a series obeys when a bucket closes. It is recorded
// per series in the dictionary rather than per sample, because the type is a
// property of the metric family and never changes between scrapes.
type Kind string

const (
	// KindCounter is monotonically cumulative: the interesting quantity over a
	// bucket is how much it grew, not the average of its readings. Histogram and
	// summary components decompose into counters, so this covers them too.
	KindCounter Kind = "c"
	// KindGauge is a point-in-time reading, averaged over a bucket.
	KindGauge Kind = "g"
	// KindUntyped is a series the exposition did not type. Treated as a gauge on
	// collapse, because averaging a value that turns out to be cumulative is
	// merely uninformative while differencing one that is not can go negative.
	KindUntyped Kind = "u"
)

// missingIndex is returned by lookup for an identity the dictionary has not seen.
const missingIndex = -1

// Entry is one series identity: what it is called, how it is labelled, and which
// collapse rule it obeys.
type Entry struct {
	Index  int               `json:"i"`
	Name   string            `json:"n"`
	Labels map[string]string `json:"l,omitempty"`
	Kind   Kind              `json:"k"`
}

// Sample is one scrape encoded against a dictionary generation.
//
// Values is positional: Values[i] is the reading for the entry at index i. A
// series the dictionary knows but this scrape did not report — a flow that was
// removed by a reload — holds NaN, which is how a gap is distinguished from a
// genuine zero.
type Sample struct {
	Gen    int       `json:"g"`
	TimeMS int64     `json:"t"`
	Values []float64 `json:"v"`
}

// Dictionary interns series identities to stable indices.
//
// Not safe for concurrent use. One sampler goroutine owns it, which is the whole
// concurrency model of this sidecar.
type Dictionary struct {
	gen     int
	byKey   map[string]int
	entries []Entry
	// dirty records whether entries were appended since the last generation bump,
	// so a scrape that reports exactly what the last one did costs no new
	// generation and no dictionary write.
	dirty bool
}

// NewDictionary returns an empty dictionary at generation 0.
func NewDictionary() *Dictionary {
	return &Dictionary{byKey: make(map[string]int)}
}

// Gen is the current generation.
func (d *Dictionary) Gen() int { return d.gen }

// Len is how many identities the dictionary holds.
func (d *Dictionary) Len() int { return len(d.entries) }

// Entries returns every identity, in index order. The slice is the dictionary's
// own; callers read it and do not retain it past the next Encode.
func (d *Dictionary) Entries() []Entry { return d.entries }

// Kind returns the collapse rule for an index, and false when the index is not
// in the dictionary.
func (d *Dictionary) Kind(index int) (Kind, bool) {
	if index < 0 || index >= len(d.entries) {
		return "", false
	}
	return d.entries[index].Kind, true
}

// Dirty reports whether identities were appended since the last Encode returned
// a bumped generation — that is, whether the dictionary needs writing again.
func (d *Dictionary) Dirty() bool { return d.dirty }

// MarkClean records that the current generation has been persisted.
func (d *Dictionary) MarkClean() { d.dirty = false }

// intern returns the index for an identity, appending it when new.
func (d *Dictionary) intern(name string, labels map[string]string, kind Kind) int {
	key := identityKey(name, labels)
	if i, ok := d.byKey[key]; ok {
		return i
	}
	i := len(d.entries)
	d.byKey[key] = i
	d.entries = append(d.entries, Entry{Index: i, Name: name, Labels: labels, Kind: kind})
	d.dirty = true
	return i
}

// lookup returns the index for an identity, or missingIndex.
func (d *Dictionary) lookup(name string, labels map[string]string) int {
	if i, ok := d.byKey[identityKey(name, labels)]; ok {
		return i
	}
	return missingIndex
}

// identityKey is the map key for a series identity: the metric name followed by
// its labels in sorted order. Sorted because a scrape's label order is not
// guaranteed stable and two orderings of the same labels are one series.
func identityKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	names := make([]string, 0, len(labels))
	for k := range labels {
		names = append(names, k)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(name)
	for _, k := range names {
		b.WriteByte(0x1f) // unit separator: cannot occur in a label name or value
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

// labelsOf copies a metric's label pairs into a map, returning nil for an
// unlabelled series so an unlabelled Entry marshals without an empty object.
func labelsOf(m *dto.Metric) map[string]string {
	if len(m.GetLabel()) == 0 {
		return nil
	}
	out := make(map[string]string, len(m.GetLabel()))
	for _, p := range m.GetLabel() {
		out[p.GetName()] = p.GetValue()
	}
	return out
}
