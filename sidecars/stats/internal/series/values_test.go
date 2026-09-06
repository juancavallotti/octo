package series

import (
	"encoding/json"
	"math"
	"math/rand/v2"
	"testing"
)

// The bug this type exists for: encoding/json refuses NaN outright, so a plain
// []float64 cannot carry the one value this format uses to mean "gap". Written
// against a Sample rather than a bare Values because the Sample is what the
// store marshals, and the field's type is the thing that has to be right.
func TestSampleWithAGapEncodes(t *testing.T) {
	in := Sample{Gen: 2, TimeMS: 1700000000000, Values: Values{1.5, math.NaN(), 3}}

	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal a sample holding a gap: %v", err)
	}
	if want := `{"g":2,"t":1700000000000,"v":[1.5,null,3]}`; string(encoded) != want {
		t.Errorf("encoded = %s, want %s", encoded, want)
	}

	var out Sample
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Values) != 3 {
		t.Fatalf("decoded %d values, want 3", len(out.Values))
	}
	if out.Values[0] != 1.5 || out.Values[2] != 3 {
		t.Errorf("readings = %v, want the ones written", out.Values)
	}
	// A gap has to come back as a gap. Decoding null to zero would turn "this
	// series was not reported" into "this series read zero", which is the
	// distinction the NaN was carrying in the first place.
	if !math.IsNaN(out.Values[1]) {
		t.Errorf("gap decoded to %v, want NaN", out.Values[1])
	}
}

// The infinities are folded in with NaN rather than left to fail the write.
func TestInfinitiesEncodeAsGaps(t *testing.T) {
	encoded, err := json.Marshal(Values{math.Inf(1), math.Inf(-1)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `[null,null]`; string(encoded) != want {
		t.Errorf("encoded = %s, want %s", encoded, want)
	}
}

// The ordinary case has to stay byte-identical to what a []float64 produced,
// because the measured per-sample cost in the docs was taken against that and
// a wider float format would quietly invalidate it.
func TestFiniteValuesEncodeAsPlainNumbers(t *testing.T) {
	encoded, err := json.Marshal(Values{0, -1, 0.5, 1e21, 1234567.125})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var plain []float64
	if err := json.Unmarshal(encoded, &plain); err != nil {
		t.Fatalf("a plain []float64 could not read it back: %v", err)
	}

	reference, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("marshal reference: %v", err)
	}
	if string(encoded) != string(reference) {
		t.Errorf("encoded = %s, want %s (what encoding/json writes for the same numbers)",
			encoded, reference)
	}
}

// appendFloat reproduces encoding/json's number formatting by hand, which is
// exactly the kind of code that is right for the cases someone thought of. This
// walks the boundaries it switches on and a spread of random magnitudes, and
// compares against the encoder itself.
func TestFloatFormatMatchesEncodingJSON(t *testing.T) {
	values := []float64{
		0, -0, 1, -1, 0.5, 100, 1e-7, 1e-6, 9.999999e-7, 1e20, 1e21, 9.999999e20,
		math.MaxFloat64, math.SmallestNonzeroFloat64, -math.MaxFloat64,
		1234567.125, 1e-9, 2.5e-10, 1 / 3.0, 1e6, 1e7,
	}
	// Random magnitudes across the whole exponent range, so the boundaries are
	// not the only thing covered.
	rng := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		exp := rng.IntN(80) - 40
		values = append(values, (rng.Float64()*2-1)*math.Pow(10, float64(exp)))
	}

	for _, f := range values {
		want, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("reference marshal %v: %v", f, err)
		}
		if got := string(appendFloat(nil, f)); got != string(want) {
			t.Errorf("appendFloat(%v) = %s, encoding/json writes %s", f, got, want)
		}
	}
}

// A nil vector is not an empty one: it means the row carried no values at all.
func TestNilRoundTrips(t *testing.T) {
	encoded, err := json.Marshal(Values(nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != "null" {
		t.Errorf("encoded = %s, want null", encoded)
	}

	var out Values
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != nil {
		t.Errorf("decoded = %v, want nil", out)
	}
}

func TestEmptyRoundTrips(t *testing.T) {
	encoded, err := json.Marshal(Values{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(encoded) != "[]" {
		t.Errorf("encoded = %s, want []", encoded)
	}
}
