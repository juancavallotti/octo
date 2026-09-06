// Package podstats reads the per-pod metrics the stats sidecar writes to Redis
// and turns them back into named, labelled, typed series.
//
// # Why the types are duplicated
//
// The writer is sidecars/stats, a separate Go module with no go.work and no
// replace directive, and its types live under internal/. This module cannot
// import them and never will. So Entry, Sample and Bucket are redeclared here
// with byte-identical JSON tags, and wire_contract_test.go reads the writer's
// source and compares the declarations — the same device
// runtime/services/k8s/rediskv_contract_test.go uses for the volatile KV
// layout, and for the same reason: two copies that drift do not fail to
// compile, they silently disagree.
//
// # What is stored
//
// A sample is [gen, timeMs, v0, v1, ...] and the numbers mean nothing without
// the dictionary beside them. Each series identity is interned once into
// dict:{gen} and a row carries only the readings, positional to it. That
// encoding exists because self-describing JSON is six to ten times larger and
// would not fit the shared cache — it is the right storage shape and the wrong
// API shape, which is why this package exists to undo it.
//
// Indices are only ever appended, so a later generation is a superset of every
// earlier one and the newest dictionary decodes older rows too.
//
// A gap — a series the dictionary knows that the scrape did not report — is
// null on the wire and NaN in memory. It is not a zero, and the distinction
// matters: a removed flow reads as absent rather than as idle.
package podstats

import (
	"encoding/json"
	"math"
)

// Kind is the collapse rule a series obeys, recorded once per series in the
// dictionary rather than per row. The single-letter values are the writer's.
type Kind string

const (
	// KindCounter is cumulative, so what a bucket stores is how much it grew.
	KindCounter Kind = "c"
	// KindGauge is a point-in-time reading, averaged over a bucket.
	KindGauge Kind = "g"
	// KindUntyped is a series the exposition did not type. Collapsed as a gauge.
	KindUntyped Kind = "u"
)

// String renders a kind the way the API spells it, rather than as the letter
// the storage uses. Callers should not have to learn the storage alphabet.
func (k Kind) String() string {
	switch k {
	case KindCounter:
		return "counter"
	case KindGauge:
		return "gauge"
	case KindUntyped:
		return "untyped"
	default:
		return "unknown"
	}
}

// Entry is one series identity: what it is called, how it is labelled, and
// which collapse rule it obeys.
//
// Labels is omitempty, so an unlabelled series has no "l" key at all and
// decodes to a nil map rather than an empty one.
type Entry struct {
	Index  int               `json:"i"`
	Name   string            `json:"n"`
	Labels map[string]string `json:"l,omitempty"`
	Kind   Kind              `json:"k"`
}

// Sample is one scrape encoded against a dictionary generation.
//
// Values is positional: Values[i] is the reading for the entry at index i. A
// series the dictionary knows but the scrape did not report holds NaN.
type Sample struct {
	Gen    int    `json:"g"`
	TimeMS int64  `json:"t"`
	Values Values `json:"v"`
}

// Bucket is one collapsed interval of the history tier.
//
// The four slices are parallel and indexed by dictionary index, exactly as a
// Sample's values are. What each holds depends on the series' kind, which is
// why the dictionary records the kind and this does not repeat it: Value is a
// delta for a counter and a mean for a gauge, and Last for a counter is its
// closing absolute value — the number that stitches consecutive buckets back
// into a cumulative series.
//
// EndMS is exclusive. Rows are not contiguous: a scrape gap closes only the
// bucket that had data, so ends[i] and the next row's start can differ.
type Bucket struct {
	Gen     int    `json:"g"`
	StartMS int64  `json:"t"`
	EndMS   int64  `json:"e"`
	Samples int    `json:"n"`
	Value   Values `json:"v"`
	Min     Values `json:"mn"`
	Max     Values `json:"mx"`
	Last    Values `json:"l"`
}

// Values is a positional vector of readings, with null for a gap.
//
// Only the decoding half of the writer's type is needed here — this module
// never writes these rows — but null has to become NaN rather than zero, which
// is the whole reason the writer stopped using a plain []float64.
type Values []float64

// UnmarshalJSON reads an array of numbers and nulls, turning every null into
// NaN so an absent reading stays distinguishable from a zero one.
func (v *Values) UnmarshalJSON(data []byte) error {
	var raw []*float64
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		*v = nil
		return nil
	}

	out := make(Values, len(raw))
	for i, f := range raw {
		if f == nil {
			out[i] = math.NaN()
			continue
		}
		out[i] = *f
	}
	*v = out
	return nil
}

// At returns the reading at a dictionary index, and whether it is a real one.
//
// Bounds are checked rather than assumed because the two lengths genuinely
// disagree in normal operation: a row written at generation 2 is shorter than
// the generation-5 dictionary fetched to decode it, and a row written between
// the dictionary read and the row read is longer. Both are ordinary, and
// neither is a reason to panic or to report a zero.
func (v Values) At(i int) (float64, bool) {
	if i < 0 || i >= len(v) {
		return 0, false
	}
	f := v[i]
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}
