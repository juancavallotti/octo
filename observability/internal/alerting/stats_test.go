package alerting

import (
	"math"
	"testing"
)

const tolerance = 1e-4

func closeTo(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestMedian(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		want   float64
		ok     bool
	}{
		{"empty", nil, 0, false},
		{"one", []float64{7}, 7, true},
		{"odd", []float64{3, 1, 2}, 2, true},
		{"even", []float64{4, 1, 3, 2}, 2.5, true},
		{"unsorted with repeats", []float64{5, 1, 1, 5}, 3, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Median(c.values)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok {
				closeTo(t, got, c.want, "median")
			}
		})
	}
}

// The input must survive: callers hand over slices they still hold, and a
// median that sorted in place would silently reorder a caller's window.
func TestMedianDoesNotMutateItsInput(t *testing.T) {
	values := []float64{3, 1, 2}
	if _, ok := Median(values); !ok {
		t.Fatal("median refused a non-empty sample")
	}
	if values[0] != 3 || values[1] != 1 || values[2] != 2 {
		t.Errorf("input was reordered: %v", values)
	}
}

func TestMADIsZeroForAnIdenticalSample(t *testing.T) {
	median, mad, ok := MAD([]float64{4, 4, 4, 4})
	if !ok {
		t.Fatal("MAD refused a non-empty sample")
	}
	closeTo(t, median, 4, "median")
	closeTo(t, mad, 0, "mad")
}

func TestMADIgnoresAContaminatedMinority(t *testing.T) {
	// Eleven quiet buckets and one enormous one. A standard deviation would be
	// dragged up by the outlier and would then find the next outlier ordinary;
	// the MAD must not move at all.
	quiet := []float64{2, 2, 3, 2, 3, 2, 2, 3, 2, 2, 3}
	_, before, _ := MAD(quiet)
	_, after, _ := MAD(append(append([]float64(nil), quiet...), 900))
	closeTo(t, after, before, "mad with an outlier")
}

func TestScaleFloors(t *testing.T) {
	// A quiet count series: MAD is zero, so only the Poisson floor stops a
	// division by zero — and it is what makes 1 to 3 unremarkable.
	closeTo(t, Scale(1, 0, true, 0), 1, "poisson floor at a median of 1")
	closeTo(t, Scale(100, 0, true, 0), 10, "poisson floor at a median of 100")

	// Not count-like, so no Poisson floor; the operator's own floor is all there is.
	closeTo(t, Scale(0.02, 0, false, 0.01), 0.01, "operator floor")

	// A real MAD beats both floors.
	closeTo(t, Scale(10, 10, false, 0.5), 14.826, "mad-derived scale")
}

// The numbers in this test are the reason the Wilson bound is here at all: at two
// samples the bound is far below any threshold anyone would set, and at four
// hundred it is close enough to the point estimate to fire.
func TestWilsonLowerBound(t *testing.T) {
	lo, ok := WilsonLowerBound(1, 2, 0.95)
	if !ok {
		t.Fatal("bound refused two trials")
	}
	closeTo(t, lo, 0.0945, "wilson lower at n=2 k=1")
	if lo > 0.10 {
		t.Errorf("a 50%% rate from two requests would clear a 10%% threshold: %v", lo)
	}

	lo, _ = WilsonLowerBound(200, 400, 0.95)
	closeTo(t, lo, 0.4512, "wilson lower at n=400 k=200")
	if lo < 0.10 {
		t.Errorf("a 50%% rate from four hundred requests must clear a 10%% threshold: %v", lo)
	}
}

func TestWilsonWithNoTrialsIsUndefined(t *testing.T) {
	if _, ok := WilsonLowerBound(0, 0, 0.95); ok {
		t.Error("a proportion of nothing reported a bound")
	}
}

func TestWilsonBoundsBracketThePointEstimate(t *testing.T) {
	lo, _ := WilsonLowerBound(30, 100, 0.95)
	hi, _ := WilsonUpperBound(30, 100, 0.95)
	if !(lo < 0.30 && 0.30 < hi) {
		t.Errorf("bounds [%v, %v] do not bracket 0.30", lo, hi)
	}
	if lo < 0 || hi > 1 {
		t.Errorf("bounds [%v, %v] escaped [0, 1]", lo, hi)
	}
}

// A wider interval must be more reluctant to fire, in both directions.
func TestConfidenceWidensTheInterval(t *testing.T) {
	lo95, _ := WilsonLowerBound(30, 100, 0.95)
	lo99, _ := WilsonLowerBound(30, 100, 0.99)
	if lo99 >= lo95 {
		t.Errorf("99%% lower bound %v is not below the 95%% bound %v", lo99, lo95)
	}
}

func TestUnknownConfidenceFallsBackRatherThanFailing(t *testing.T) {
	fallback, _ := WilsonLowerBound(30, 100, 0.42)
	standard, _ := WilsonLowerBound(30, 100, defaultConfidence)
	closeTo(t, fallback, standard, "bound under an unsupported confidence")
}

func TestBoundPicksTheConservativeEndPerOperator(t *testing.T) {
	lo, _ := WilsonLowerBound(1, 2, 0.95)
	hi, _ := WilsonUpperBound(1, 2, 0.95)

	got, _ := Bound(OpGT, 1, 2, 0.95)
	closeTo(t, got, lo, "bound for gt")
	got, _ = Bound(OpLT, 1, 2, 0.95)
	closeTo(t, got, hi, "bound for lt")
}
