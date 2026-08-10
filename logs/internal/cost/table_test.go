package cost

import (
	"math/rand/v2"
	"testing"
)

// shuffleRounds is how many independent orderings of the same catalogue each
// resolution case is checked against. Resolution must not depend on the order the
// feed happened to arrive in, and the input really does arrive unordered: an HTTP
// response concatenated from several providers, then a map iteration in the store.
const shuffleRounds = 10

func ptr(f float64) *float64 { return &f }

// catalogue is shaped like the real published rate card — one model under two
// providers, a family addressed by substring at two lengths, a pattern the feed
// publishes twice — but its *prices are deliberately adversarial*.
//
// Cheapest-input is the second-to-last key moreSpecific compares, so a fixture
// with realistic prices would let almost every case pass for the wrong reason:
// the rule under test and the price tiebreak would agree, and reversing the rule
// would not fail anything. Here each entry that a rule must pick is priced HIGHER
// than the entry it must beat, so only the rule itself can produce the expected
// answer.
func catalogue() []Rate {
	return []Rate{
		// Isolates operator specificity: the exact name is dearer than the
		// substring covering the same family, so only "equals wins" picks it.
		{
			ID: "anthropic-sonnet", Provider: "ANTHROPIC", Pattern: "claude-3-5-sonnet-20241022",
			Operator: OpEquals, InputPer1M: 3, OutputPer1M: 15,
			CacheWritePer1M: ptr(3.75), CacheReadPer1M: ptr(0.3), Preferred: true,
		},
		{
			ID: "anthropic-family", Provider: "ANTHROPIC", Pattern: "claude-3-5-sonnet",
			Operator: OpIncludes, InputPer1M: 2, OutputPer1M: 10,
		},

		// Isolates startsWith outranking includes: the prefix entry is both
		// shorter and dearer than the substring entry it must beat.
		{
			ID: "openai-o-series", Provider: "OPENAI", Pattern: "o3-",
			Operator: OpStartsWith, InputPer1M: 2, OutputPer1M: 8,
		},
		{
			ID: "openai-o3-mini-sub", Provider: "OPENAI", Pattern: "o3-mini",
			Operator: OpIncludes, InputPer1M: 1, OutputPer1M: 4,
		},

		// Isolates provider preference: AZURE is cheaper AND flagged current, so
		// only "prefer the vendor we have a connector for" picks OPENAI.
		{
			ID: "openai-4o", Provider: "OPENAI", Pattern: "gpt-4o",
			Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10, CacheReadPer1M: ptr(1.25),
		},
		{
			ID: "azure-4o", Provider: "AZURE", Pattern: "gpt-4o",
			Operator: OpEquals, InputPer1M: 2, OutputPer1M: 8, Preferred: true,
		},

		// Isolates the catalogue's current-price flag: the flagged row is the
		// dearer of the two, so only the flag picks it.
		{
			ID: "azure-mini-stale", Provider: "AZURE", Pattern: "gpt-4o-mini",
			Operator: OpEquals, InputPer1M: 0.15, OutputPer1M: 0.6,
		},
		{
			ID: "azure-mini-current", Provider: "AZURE", Pattern: "gpt-4o-mini",
			Operator: OpEquals, InputPer1M: 0.2, OutputPer1M: 0.8, Preferred: true,
		},

		// Isolates longest-pattern: the longer substring is the dearer one, so
		// only pattern length picks it.
		{
			ID: "google-flash", Provider: "GOOGLE", Pattern: "gemini-2.5-flash",
			Operator: OpIncludes, InputPer1M: 0.1, OutputPer1M: 0.4, CacheReadPer1M: ptr(0.075),
		},
		{
			ID: "google-flash-lite", Provider: "GOOGLE", Pattern: "gemini-2.5-flash-lite",
			Operator: OpIncludes, InputPer1M: 0.3, OutputPer1M: 2.5,
		},
	}
}

func TestTableRate(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  string // the ID of the entry that must win, "" for no match
	}{
		{
			name:  "an exact name beats a substring of the same family",
			model: "claude-3-5-sonnet-20241022",
			want:  "anthropic-sonnet",
		},
		{
			name:  "a substring still answers an id the card does not name exactly",
			model: "claude-3-5-sonnet-latest",
			want:  "anthropic-family",
		},
		{
			name:  "a prefix beats a substring even when the substring is longer",
			model: "o3-mini-2025-01-31",
			want:  "openai-o-series",
		},
		{
			name:  "the longer of two matching substrings wins",
			model: "gemini-2.5-flash-lite-preview-06-17",
			want:  "google-flash-lite",
		},
		{
			name:  "the shorter substring still answers what the longer does not match",
			model: "gemini-2.5-flash-preview-05-20",
			want:  "google-flash",
		},
		{
			name:  "a model under two providers resolves to the one we have a connector for",
			model: "gpt-4o",
			want:  "openai-4o",
		},
		{
			name:  "the catalogue's own current-price flag breaks a duplicated key",
			model: "gpt-4o-mini",
			want:  "azure-mini-current",
		},
		{
			name:  "case and surrounding whitespace do not decide a match",
			model: "  GPT-4o  ",
			want:  "openai-4o",
		},
		{
			name:  "a model the card does not cover has no rate",
			model: "some-model-nobody-published",
			want:  "",
		},
		{
			name:  "an empty model has no rate",
			model: "",
			want:  "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Every case is checked against many orderings of the same catalogue,
			// because "which rate priced this call" must be a property of the card
			// and the model, not of the order the rows arrived in.
			for round := range shuffleRounds {
				rates := catalogue()
				rand.New(rand.NewPCG(uint64(round), 0)).Shuffle(len(rates), func(i, j int) {
					rates[i], rates[j] = rates[j], rates[i]
				})

				got, ok := NewTable(rates).Rate(tc.model)
				if tc.want == "" {
					if ok {
						t.Fatalf("round %d: %q resolved to %q, want no rate", round, tc.model, got.ID)
					}
					continue
				}
				if !ok {
					t.Fatalf("round %d: %q resolved to no rate, want %q", round, tc.model, tc.want)
				}
				if got.ID != tc.want {
					t.Fatalf("round %d: %q resolved to %q, want %q", round, tc.model, got.ID, tc.want)
				}
			}
		})
	}
}

func TestTableSkipsUnusableEntries(t *testing.T) {
	cases := []struct {
		name  string
		rate  Rate
		probe string
	}{
		{
			// Under includes this matches every model there is, so admitting it
			// would price the whole catalogue at one row's rate.
			name:  "an empty pattern is never admitted",
			rate:  Rate{ID: "catch-all", Provider: "OPENAI", Pattern: "", Operator: OpIncludes, InputPer1M: 99},
			probe: "gpt-4o",
		},
		{
			name:  "an operator this package cannot apply is never admitted",
			rate:  Rate{ID: "regex", Provider: "OPENAI", Pattern: "gpt-4o", Operator: "regex", InputPer1M: 99},
			probe: "gpt-4o",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := NewTable([]Rate{tc.rate})
			if got, ok := table.Rate(tc.probe); ok {
				t.Fatalf("%q resolved to %q, want no rate", tc.probe, got.ID)
			}
			if table.Len() != 0 {
				t.Fatalf("table admitted %d entries, want 0", table.Len())
			}
		})
	}
}

func TestTableSkippingOneEntryLeavesTheRest(t *testing.T) {
	table := NewTable([]Rate{
		{ID: "bad", Provider: "OPENAI", Pattern: "", Operator: OpIncludes},
		{ID: "good", Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 2.5},
	})

	if table.Len() != 1 {
		t.Fatalf("table admitted %d entries, want 1", table.Len())
	}
	got, ok := table.Rate("gpt-4o")
	if !ok || got.ID != "good" {
		t.Fatalf("gpt-4o resolved to %q (found=%v), want %q", got.ID, ok, "good")
	}
}

// A refresher holds no table until its first load lands. Reporting "no rate"
// rather than panicking is what lets ingest keep running through that window,
// recording calls as unpriced instead of dropping them.
func TestNilTableReportsNoRate(t *testing.T) {
	var table *Table
	if _, ok := table.Rate("gpt-4o"); ok {
		t.Fatal("a nil table returned a rate")
	}
	if table.Len() != 0 {
		t.Fatalf("a nil table reported %d entries, want 0", table.Len())
	}
}

// An unmatched lookup must return the zero Rate together with false, never a
// usable-looking rate of zero: a caller that ignored the boolean would otherwise
// price the call at nothing and drop it silently out of every total.
func TestUnmatchedLookupReturnsNoRates(t *testing.T) {
	got, ok := NewTable(catalogue()).Rate("nothing-like-this")
	if ok {
		t.Fatal("an unknown model reported a rate")
	}
	if got.InputPer1M != 0 || got.OutputPer1M != 0 || got.Provider != "" || got.ID != "" {
		t.Fatalf("an unknown model returned %+v, want the zero Rate", got)
	}
}
