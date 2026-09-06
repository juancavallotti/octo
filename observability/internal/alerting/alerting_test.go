package alerting

import "testing"

func TestOpCompare(t *testing.T) {
	cases := []struct {
		op       Op
		observed float64
		want     bool
	}{
		{OpGT, 6, true}, {OpGT, 5, false},
		{OpGTE, 5, true}, {OpGTE, 4, false},
		{OpLT, 4, true}, {OpLT, 5, false},
		{OpLTE, 5, true}, {OpLTE, 6, false},
		// An operator that is not one of the four compares false rather than
		// panicking. Operators are validated at save time, and an evaluator that
		// panicked on stored data would take the whole tick down with it.
		{Op("between"), 5, false},
	}
	for _, c := range cases {
		if got := c.op.Compare(c.observed, 5); got != c.want {
			t.Errorf("%s: %v vs 5 = %v, want %v", c.op, c.observed, got, c.want)
		}
	}
}

func TestDownwardOperators(t *testing.T) {
	for _, op := range []Op{OpLT, OpLTE} {
		if !op.Downward() {
			t.Errorf("%s is not recognised as downward, so a dead pipeline would satisfy it", op)
		}
	}
	for _, op := range []Op{OpGT, OpGTE} {
		if op.Downward() {
			t.Errorf("%s was treated as downward", op)
		}
	}
}

// The Kleene table, in full. The two rows that matter are the ones where an
// unknown changes the answer, because those are the cases a two-valued
// implementation gets confidently wrong.
func TestCombine(t *testing.T) {
	cases := []struct {
		name     string
		combine  Combinator
		verdicts []Truth
		want     Truth
	}{
		{"all: every one true", CombineAll, []Truth{True, True}, True},
		{"all: one false settles it", CombineAll, []Truth{True, False}, False},
		// The conjunction cannot be claimed on the strength of the conditions
		// that did answer: the operator asked for all of them.
		{"all: an unknown poisons a true", CombineAll, []Truth{True, Unknown}, Unknown},
		// But a false still settles it, unknown sibling or not — nothing the
		// missing condition could have said would rescue it.
		{"all: false beats unknown", CombineAll, []Truth{False, Unknown}, False},

		{"any: one true settles it", CombineAny, []Truth{False, True}, True},
		// A satisfied disjunct is satisfied however blind the rest of the watch was.
		{"any: true beats unknown", CombineAny, []Truth{Unknown, True}, True},
		{"any: an unknown poisons a false", CombineAny, []Truth{False, Unknown}, Unknown},
		{"any: all false", CombineAny, []Truth{False, False}, False},

		// A watch with no conditions is a mistake, and neither boolean answer
		// says so.
		{"empty is unknown, not vacuously true", CombineAll, nil, Unknown},
		{"empty any is unknown, not false", CombineAny, nil, Unknown},

		{"single true", CombineAll, []Truth{True}, True},
		{"single unknown", CombineAny, []Truth{Unknown}, Unknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Combine(c.combine, c.verdicts); got != c.want {
				t.Errorf("Combine(%s, %v) = %s, want %s", c.combine, c.verdicts, got, c.want)
			}
		})
	}
}

func TestTruthOf(t *testing.T) {
	if TruthOf(true) != True || TruthOf(false) != False {
		t.Error("TruthOf does not lift a boolean")
	}
	if Unknown.String() != "unknown" || True.String() != "true" || False.String() != "false" {
		t.Error("Truth does not name itself")
	}
}

func TestNoDataPolicy(t *testing.T) {
	cases := map[NoDataPolicy]Truth{
		NoDataOK:   False,
		NoDataFire: True,
		NoDataKeep: Unknown,
		// An unrecognised policy reads as the safe default rather than firing.
		NoDataPolicy("shrug"): False,
	}
	for policy, want := range cases {
		if got := policy.Truth(); got != want {
			t.Errorf("%s reads absence as %s, want %s", policy, got, want)
		}
	}
}
