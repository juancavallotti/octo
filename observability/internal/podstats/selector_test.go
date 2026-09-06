package podstats

import (
	"errors"
	"reflect"
	"strconv"
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
	got, err := Selector{Names: []string{"go_goroutines"}}.Resolve(histogramDict())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

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
	got, err := Selector{Names: []string{"octo_flow_latency_bucket"}}.Resolve(histogramDict())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(got) != 6 {
		t.Errorf("resolved %d series, want the 6 buckets across both flows", len(got))
	}
}

func TestSelectorNarrowsByLabel(t *testing.T) {
	dict := histogramDict()

	got, err := Selector{
		Names:  []string{"octo_flow_latency_bucket"},
		Labels: map[string]string{"flow": "a"},
	}.Resolve(dict)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("resolved %d series, want flow a's 3 buckets", len(got))
	}

	// Labels are ANDed.
	exact, err := Selector{
		Names:  []string{"octo_flow_latency_bucket"},
		Labels: map[string]string{"flow": "a", "le": "+Inf"},
	}.Resolve(dict)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
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
	got, err := Selector{Names: []string{"octo_flow_latency_bucket", "go_goroutines"}}.
		Resolve(histogramDict())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("indices = %v, want ascending", got)
		}
	}
}

// Prefix matching would read as convenience and behave as a trap: it would pull
// in every histogram bucket of every flow behind a short name.
func TestSelectorDoesNotMatchByPrefix(t *testing.T) {
	if got, _ := (Selector{Names: []string{"octo_flow"}}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v for a prefix, want nothing", got)
	}
	if got, _ := (Selector{Names: []string{"octo_flow_latency"}}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v for a partial name, want nothing", got)
	}
}

// The filter is required. Defaulting an empty one to everything would remove
// the bound the whole read strategy rests on.
func TestSelectorWithoutNamesSelectsNothing(t *testing.T) {
	if got, _ := (Selector{}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v with no names, want nothing", got)
	}
	if got, _ := (Selector{Labels: map[string]string{"flow": "a"}}).Resolve(histogramDict()); got != nil {
		t.Errorf("resolved %v from labels alone, want nothing", got)
	}
}

func TestSelectorAgainstAnEmptyDictionary(t *testing.T) {
	if got, _ := (Selector{Names: []string{"go_goroutines"}}).Resolve(nil); got != nil {
		t.Errorf("resolved %v against no dictionary, want nothing", got)
	}
}

func TestSelectorUnknownNameResolvesToNothing(t *testing.T) {
	got, err := Selector{Names: []string{"go_goroutines", "nope"}}.Resolve(histogramDict())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("resolved %v, want only the name that exists", got)
	}
}

// A label the series does not carry at all must not match, or a filter would
// silently widen instead of narrowing.
func TestSelectorLabelAbsentFromTheSeries(t *testing.T) {
	got, err := Selector{
		Names:  []string{"go_goroutines"},
		Labels: map[string]string{"flow": "a"},
	}.Resolve(histogramDict())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got != nil {
		t.Errorf("resolved %v, want nothing: go_goroutines carries no flow label", got)
	}
}

func TestSelectorIsDeterministic(t *testing.T) {
	dict := histogramDict()
	s := Selector{Names: []string{"octo_flow_latency_bucket", "octo_flow_latency_sum"}}

	first, err := s.Resolve(dict)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	for range 20 {
		if got, _ := s.Resolve(dict); !reflect.DeepEqual(got, first) {
			t.Fatalf("resolve returned %v then %v; map iteration is leaking through",
				first, got)
		}
	}
}

// An empty filter value must still require the label to be present.
//
// The absent case above passes for the wrong reason when the value is not
// empty: a missing label reads as "" from the map, so only `label=flow=`
// exposes it. That form is exactly what a UI builds from an empty input box,
// which is where it would first be met.
func TestSelectorEmptyLabelValueStillRequiresTheLabel(t *testing.T) {
	got, err := Selector{
		Names:  []string{"go_goroutines"},
		Labels: map[string]string{"flow": ""},
	}.Resolve(histogramDict())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if got != nil {
		t.Errorf("resolved %v, want nothing: go_goroutines carries no flow label "+
			"at all, so an empty value must not match it", got)
	}
}

// The cap is refused rather than truncated, and refusing is the whole point.
//
// A metric name is not a series: one histogram is a series per boundary per
// flow per outcome, so a caller naming three metrics can name hundreds. Handing
// back the first two hundred would answer a question nobody asked with a
// partial picture that looks whole.
func TestSelectorRefusesMoreSeriesThanItMayRead(t *testing.T) {
	dict := map[int]Entry{}
	for i := range MaxSelectedSeries + 1 {
		dict[i] = Entry{
			Index:  i,
			Name:   "octo_flow_latency_bucket",
			Labels: map[string]string{"le": strconv.Itoa(i)},
			Kind:   KindCounter,
		}
	}

	got, err := Selector{Names: []string{"octo_flow_latency_bucket"}}.Resolve(dict)

	if !errors.Is(err, ErrTooManySeries) {
		t.Fatalf("err = %v, want ErrTooManySeries", err)
	}
	if got != nil {
		t.Errorf("resolved %d indices alongside the error, want none", len(got))
	}
}

// Exactly at the cap is allowed: it is a maximum, not a threshold to stay under.
func TestSelectorAllowsExactlyTheCap(t *testing.T) {
	dict := map[int]Entry{}
	for i := range MaxSelectedSeries {
		dict[i] = Entry{
			Index:  i,
			Name:   "octo_flow_latency_bucket",
			Labels: map[string]string{"le": strconv.Itoa(i)},
			Kind:   KindCounter,
		}
	}

	got, err := Selector{Names: []string{"octo_flow_latency_bucket"}}.Resolve(dict)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != MaxSelectedSeries {
		t.Errorf("resolved %d, want %d", len(got), MaxSelectedSeries)
	}
}
