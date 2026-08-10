package cost

import "testing"

// stored builds a rate as it comes back from the database: carrying an id.
func stored(id, pattern string, in, out float64) Rate {
	return Rate{
		ID: id, Provider: "OPENAI", Pattern: pattern, Operator: OpEquals,
		InputPer1M: in, OutputPer1M: out,
	}
}

// incoming builds the same entry as it comes off a fetch: with no id yet.
func incoming(pattern string, in, out float64) Rate {
	return Rate{
		Provider: "OPENAI", Pattern: pattern, Operator: OpEquals,
		InputPer1M: in, OutputPer1M: out,
	}
}

func TestDiff(t *testing.T) {
	cases := []struct {
		name     string
		stored   []Rate
		fetched  []Rate
		want     []Change
		whyEmpty string
	}{
		{
			name:    "an entry the card has never carried is opened",
			stored:  nil,
			fetched: []Rate{incoming("gpt-4o", 2.5, 10)},
			want:    []Change{{Rate: incoming("gpt-4o", 2.5, 10)}},
		},
		{
			name:    "a changed price closes the old row and opens a new one",
			stored:  []Rate{stored("row-1", "gpt-4o", 5, 15)},
			fetched: []Rate{incoming("gpt-4o", 2.5, 10)},
			want:    []Change{{Rate: incoming("gpt-4o", 2.5, 10), Supersedes: "row-1"}},
		},
		{
			name:     "an unchanged price writes nothing at all",
			stored:   []Rate{stored("row-1", "gpt-4o", 2.5, 10)},
			fetched:  []Rate{incoming("gpt-4o", 2.5, 10)},
			whyEmpty: "a refresh that changed nothing must not touch the history",
		},
		{
			name:     "an entry the fetch no longer carries is left open",
			stored:   []Rate{stored("row-1", "gpt-4o", 2.5, 10)},
			fetched:  nil,
			whyEmpty: "a model vanishing from a catalogue must not unprice the calls it explains",
		},
		{
			name:    "only the changed entry is written",
			stored:  []Rate{stored("row-1", "gpt-4o", 2.5, 10), stored("row-2", "gpt-4o-mini", 0.2, 0.6)},
			fetched: []Rate{incoming("gpt-4o", 2.5, 10), incoming("gpt-4o-mini", 0.15, 0.6)},
			want:    []Change{{Rate: incoming("gpt-4o-mini", 0.15, 0.6), Supersedes: "row-2"}},
		},
		{
			name:   "the same pattern under a different operator is a different entry",
			stored: []Rate{stored("row-1", "gpt-4o", 2.5, 10)},
			fetched: []Rate{{
				Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpIncludes,
				InputPer1M: 2.5, OutputPer1M: 10,
			}},
			want: []Change{{Rate: Rate{
				Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpIncludes,
				InputPer1M: 2.5, OutputPer1M: 10,
			}}},
		},
		{
			name:   "the same pattern under a different provider is a different entry",
			stored: []Rate{stored("row-1", "gpt-4o", 2.5, 10)},
			fetched: []Rate{{
				Provider: "AZURE", Pattern: "gpt-4o", Operator: OpEquals,
				InputPer1M: 2.5, OutputPer1M: 10,
			}},
			want: []Change{{Rate: Rate{
				Provider: "AZURE", Pattern: "gpt-4o", Operator: OpEquals,
				InputPer1M: 2.5, OutputPer1M: 10,
			}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Diff(tc.stored, tc.fetched)

			if len(got) != len(tc.want) {
				t.Fatalf("got %d changes, want %d (%s)", len(got), len(tc.want), tc.whyEmpty)
			}
			for i, want := range tc.want {
				if got[i].Supersedes != want.Supersedes {
					t.Errorf("change %d supersedes %q, want %q", i, got[i].Supersedes, want.Supersedes)
				}
				if !samePrice(got[i].Rate, want.Rate) || keyOf(got[i].Rate) != keyOf(want.Rate) {
					t.Errorf("change %d rate = %+v, want %+v", i, got[i].Rate, want.Rate)
				}
			}
		})
	}
}

// A cache rate appearing or disappearing changes what a call costs even when the
// number is zero, because pricing reads absent and zero differently.
func TestDiffTreatsAppearingCacheRateAsAChange(t *testing.T) {
	cases := []struct {
		name         string
		storedCache  *float64
		fetchedCache *float64
		wantChange   bool
	}{
		{name: "absent stays absent", storedCache: nil, fetchedCache: nil, wantChange: false},
		{name: "a rate appears", storedCache: nil, fetchedCache: ptr(0.3), wantChange: true},
		{name: "a rate disappears", storedCache: ptr(0.3), fetchedCache: nil, wantChange: true},
		{name: "a rate changes", storedCache: ptr(0.3), fetchedCache: ptr(0.15), wantChange: true},
		{name: "a rate stays put", storedCache: ptr(0.3), fetchedCache: ptr(0.3), wantChange: false},
		{
			// The published number being zero does not make it the same as no
			// published number: one prices cache reads at nothing, the other
			// makes them fall back to the input rate.
			name:        "a published zero is not the same as nothing published",
			storedCache: nil, fetchedCache: ptr(0), wantChange: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			was := stored("row-1", "gpt-4o", 2.5, 10)
			was.CacheReadPer1M = tc.storedCache
			now := incoming("gpt-4o", 2.5, 10)
			now.CacheReadPer1M = tc.fetchedCache

			got := Diff([]Rate{was}, []Rate{now})
			if tc.wantChange && len(got) != 1 {
				t.Fatalf("got %d changes, want 1", len(got))
			}
			if !tc.wantChange && len(got) != 0 {
				t.Fatalf("got %d changes, want 0", len(got))
			}
		})
	}
}

// The feed's current-price flag decides which of a duplicated key is kept, so a
// flag moving is a real change even when neither price moves.
func TestDiffTreatsTheCurrentPriceFlagAsAChange(t *testing.T) {
	was := stored("row-1", "gpt-4o", 2.5, 10)
	now := incoming("gpt-4o", 2.5, 10)
	now.Preferred = true

	if got := Diff([]Rate{was}, []Rate{now}); len(got) != 1 {
		t.Fatalf("got %d changes, want 1", len(got))
	}
}

// A rate published with more precision than the column can hold would otherwise
// differ from its own stored copy forever: every refresh would close a row and
// open an identical one, growing the history without bound and re-pointing the
// price id recorded on everything priced in between.
func TestDiffIsStableAgainstTheStoredScale(t *testing.T) {
	const overPrecise = 0.12345678901234
	published := incoming("gpt-4o", overPrecise, 10)

	first := Diff(nil, []Rate{published})
	if len(first) != 1 {
		t.Fatalf("got %d changes on the first fetch, want 1", len(first))
	}

	// What the store would hold, having rounded to the column's scale.
	held := first[0].Rate
	held.ID = "row-1"

	if again := Diff([]Rate{held}, []Rate{published}); len(again) != 0 {
		t.Fatalf("re-fetching the same published rate produced %d changes, want 0", len(again))
	}
	if held.InputPer1M == overPrecise {
		t.Fatal("the rate was stored unrounded; the stored scale is not being applied")
	}
}

// The same fetch must produce the same changes in the same order, or a refresh
// becomes a source of churn in its own right.
func TestDiffIsDeterministic(t *testing.T) {
	fetched := []Rate{
		incoming("gpt-4o", 2.5, 10),
		incoming("gpt-4o-mini", 0.15, 0.6),
		incoming("o3-mini", 1.1, 4.4),
	}
	was := []Rate{stored("row-1", "gpt-4o", 5, 15)}

	first := Diff(was, fetched)
	for range shuffleRounds {
		again := Diff(was, fetched)
		if len(again) != len(first) {
			t.Fatalf("got %d changes, want %d", len(again), len(first))
		}
		for i := range first {
			if again[i].Rate.Pattern != first[i].Rate.Pattern || again[i].Supersedes != first[i].Supersedes {
				t.Fatalf("change %d differs between runs: %+v vs %+v", i, again[i], first[i])
			}
		}
	}
}
