package podstats

import (
	"errors"
	"fmt"
	"sort"
)

// ErrTooManySeries is returned when a selector matches more series than one
// query may carry. It is the caller's cue to narrow, so the handler turns it
// into a 400 rather than a 500.
var ErrTooManySeries = errors.New("too many series selected")

// MaxSelectedSeries caps how many dictionary indices one query may resolve to.
//
// A metric name is not one series. octo_flow_latency_bucket expands to a series
// per bucket boundary per flow, so a caller naming three metrics can
// accidentally name two hundred series. Refusing is better than answering
// slowly: the caller can narrow with labels, and the alternative is a response
// nobody asked the size of.
const MaxSelectedSeries = 200

// Selector picks series out of a dictionary by name and label.
//
// Names match exactly rather than by prefix. A prefix match reads as
// convenience and behaves as a trap — "octo_flow" would silently pull in every
// histogram bucket of every flow — and the metrics catalogue exists so a caller
// can find the exact names first.
type Selector struct {
	// Names is the set of metric names to include. Empty selects nothing: this
	// is a required filter, and defaulting it to everything would remove the
	// bound the whole read strategy rests on.
	Names []string
	// Labels narrows within those names. All must match.
	Labels map[string]string
}

// Resolve returns the dictionary indices the selector matches, ascending.
//
// Ascending because the values arrays are positional and reading them in index
// order is what keeps the decode a single forward pass.
//
// Refuses rather than truncates past MaxSelectedSeries. A truncated answer is
// the failure this bound exists to prevent: the caller asked about series it
// would not be told were missing, and would chart a partial picture as a whole
// one.
func (s Selector) Resolve(dict map[int]Entry) ([]int, error) {
	if len(s.Names) == 0 || len(dict) == 0 {
		return nil, nil
	}

	wanted := make(map[string]struct{}, len(s.Names))
	for _, name := range s.Names {
		wanted[name] = struct{}{}
	}

	var out []int
	for index, entry := range dict {
		if _, ok := wanted[entry.Name]; !ok {
			continue
		}
		if !matchesLabels(entry, s.Labels) {
			continue
		}
		out = append(out, index)
	}
	if len(out) > MaxSelectedSeries {
		return nil, fmt.Errorf("%w: %d match, at most %d may be read at once; "+
			"narrow with labels or ask for fewer metrics",
			ErrTooManySeries, len(out), MaxSelectedSeries)
	}
	sort.Ints(out)
	return out, nil
}

// matchesLabels reports whether an entry carries every label the filter names,
// with the value it names.
func matchesLabels(entry Entry, labels map[string]string) bool {
	for key, want := range labels {
		// Presence first. A missing label reads as "" from the map, so a filter
		// of label=flow= would otherwise match every series that has no flow
		// label at all — the opposite of narrowing.
		got, ok := entry.Labels[key]
		if !ok || got != want {
			return false
		}
	}
	return true
}
