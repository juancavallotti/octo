package podstats

import (
	"reflect"
	"testing"
)

// histogramDict is what one histogram family actually expands to: a series per
// bucket boundary plus the sum and count, all counters, across two flows. It is
// the reason a metric name is not a series and the reason label filtering
// exists.
func histogramDict() map[int]Entry {
	dict := map[int]Entry{}
	index := 0
	for _, flow := range []string{"a", "b"} {
		for _, le := range []string{"0.005", "0.01", "+Inf"} {
			dict[index] = Entry{
				Index:  index,
				Name:   "octo_flow_latency_bucket",
				Labels: map[string]string{"flow": flow, "le": le},
				Kind:   KindCounter,
			}
			index++
		}
		dict[index] = Entry{
			Index: index, Name: "octo_flow_latency_sum",
			Labels: map[string]string{"flow": flow}, Kind: KindCounter,
		}
		index++
	}
	dict[index] = Entry{Index: index, Name: "go_goroutines", Kind: KindGauge}
	return dict
}

func TestSelectorMatchesByName(t *testing.T) {
	got := Selector{Names: []string{"go_goroutines"}}.Resolve(histogramDict())

	if len(got) != 1 {
		t.Fatalf("resolved %v, want one index", got)
	}
	if histogramDict()[got[0]].Name != "go_goroutines" {
		t.Errorf("resolved the wrong series: %+v", histogramDict()[got[0]])
	}
}

// One name is eight series here. That is the arithmetic maxSelectedSeries
// exists for.
func TestSelectorExpandsAHistogramName(t *testing.T) {
	got := Selector{Names: []string{"octo_flow_latency_bucket"}}.Resolve(histogramDict())

	if len(got) != 6 {
		t.Errorf("resolved %d series, want the 6 buckets across both flows", len(got))
	}
}

func TestSelectorNarrowsByLabel(t *testing.T) {
	dict := histogramDict()

	got := Selector{
		Names:  []string{"octo_flow_latency_bucket"},
		Labels: map[string]string{"flow": "a"},
	}.Resolve(dict)
	if len(got) != 3 {
		t.Errorf("resolved %d series, want flow a's 3 buckets", len(got))
	}

	// Labels are ANDed.
	exact := Selector{
		Names:  []string{"octo_flow_latency_bucket"},
		Labels: map[string]string{"flow": "a", "le": "+Inf"},
	}.Resolve(dict)
	if len(exact) != 1 {
		t.Fatalf("resolved %d series, want exactly one", len(exact))
	}
	if entry := dict[exact[0]]; entry.Labels["le"] != "+Inf" || entry.Labels["flow"] != "a" {
		t.Errorf("resolved %+v, want flow a's +Inf bucket", entry)
	}
}

// Indices come back ascending because the values arrays are positional, and
// reading them in order is what keeps the decode a single forward pass.
func TestSelectorReturnsAscendingIndices(t *testing.T) {
	got := Selector{Names: []string{"octo_flow_latency_bucket", "go_goroutines"}}.
		Resolve(histogramDict())

	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("indices = %v, want ascending", got)
		}
	}
}

// Prefix matching would read as convenience and behave as a trap: it would pull
// in every histogram bucket of every flow behind a short name.
func TestSelectorDoesNotMatchByPrefix(t *testing.T) {
	if got := (Selector{Names: []string{"octo_flow"}}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v for a prefix, want nothing", got)
	}
	if got := (Selector{Names: []string{"octo_flow_latency"}}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v for a partial name, want nothing", got)
	}
}

// The filter is required. Defaulting an empty one to everything would remove
// the bound the whole read strategy rests on.
func TestSelectorWithoutNamesSelectsNothing(t *testing.T) {
	if got := (Selector{}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v with no names, want nothing", got)
	}
	if got := (Selector{Labels: map[string]string{"flow": "a"}}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v from labels alone, want nothing", got)
	}
}

func TestSelectorAgainstAnEmptyDictionary(t *testing.T) {
	if got := (Selector{Names: []string{"go_goroutines"}}).Resolve(nil); got != nil {
		t.Errorf("resolved %v against no dictionary, want nothing", got)
	}
}

func TestSelectorUnknownNameResolvesToNothing(t *testing.T) {
	got := Selector{Names: []string{"go_goroutines", "nope"}}.Resolve(histogramDict())
	if len(got) != 1 {
		t.Errorf("resolved %v, want only the name that exists", got)
	}
}

// A label the series does not carry at all must not match, or a filter would
// silently widen instead of narrowing.
func TestSelectorLabelAbsentFromTheSeries(t *testing.T) {
	got := Selector{
		Names:  []string{"go_goroutines"},
		Labels: map[string]string{"flow": "a"},
	}.Resolve(histogramDict())

	if got != nil {
		t.Errorf("resolved %v, want nothing: go_goroutines carries no flow label", got)
	}
}

func TestSelectorIsDeterministic(t *testing.T) {
	dict := histogramDict()
	s := Selector{Names: []string{"octo_flow_latency_bucket", "octo_flow_latency_sum"}}

	first := s.Resolve(dict)
	for range 20 {
		if got := s.Resolve(dict); !reflect.DeepEqual(got, first) {
			t.Fatalf("resolve returned %v then %v; map iteration is leaking through",
				first, got)
		}
	}
}
