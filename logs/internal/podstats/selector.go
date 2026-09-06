package podstats

import "sort"

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
func (s Selector) Resolve(dict map[int]Entry) []int {
	if len(s.Names) == 0 || len(dict) == 0 {
		return nil
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
	sort.Ints(out)
	return out
}

// matchesLabels reports whether an entry carries every label the filter names,
// with the value it names.
func matchesLabels(entry Entry, labels map[string]string) bool {
	for key, want := range labels {
		if entry.Labels[key] != want {
			return false
		}
	}
	return true
}
