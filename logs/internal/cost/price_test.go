package cost

import (
	"math"
	"testing"
)

// epsilon bounds the float comparison. Costs are fractions of a cent built from
// divisions by a million, so exact equality would be asserting the shape of IEEE
// rounding rather than the arithmetic.
const epsilon = 1e-12

// priceCard covers one model per provider convention that changes the arithmetic,
// plus a rate that publishes no cache price and an embedding rate.
func priceCard() *Table {
	return NewTable([]Rate{
		{
			ID: "anthropic", Provider: "ANTHROPIC", Pattern: "claude-x", Operator: OpEquals,
			InputPer1M: 3, OutputPer1M: 15, CacheReadPer1M: ptr(0.3),
		},
		{
			ID: "anthropic-nocache", Provider: "ANTHROPIC", Pattern: "claude-nocache", Operator: OpEquals,
			InputPer1M: 3, OutputPer1M: 15,
		},
		{
			ID: "openai", Provider: "OPENAI", Pattern: "gpt-x", Operator: OpEquals,
			InputPer1M: 2.5, OutputPer1M: 10, CacheReadPer1M: ptr(1.25),
		},
		{
			ID: "openai-nocache", Provider: "OPENAI", Pattern: "gpt-nocache", Operator: OpEquals,
			InputPer1M: 2, OutputPer1M: 4,
		},
		{
			ID: "google", Provider: "GOOGLE", Pattern: "gemini-x", Operator: OpEquals,
			InputPer1M: 0.3, OutputPer1M: 2.5, CacheReadPer1M: ptr(0.075),
		},
		{
			ID: "acme", Provider: "ACME", Pattern: "acme-x", Operator: OpEquals,
			InputPer1M: 1, OutputPer1M: 2, CacheReadPer1M: ptr(0.1),
		},
		{
			ID: "embed", Provider: "OPENAI", Pattern: "text-embedding-x", Operator: OpEquals,
			InputPer1M: 0.02, OutputPer1M: 0,
		},
	})
}

func TestPrice(t *testing.T) {
	cases := []struct {
		name       string
		call       Call
		wantStatus Status
		wantCost   float64
		wantKnown  bool
	}{
		{
			// Anthropic reports the uncached remainder, so cached tokens are
			// additional to the input count rather than part of it.
			name: "anthropic charges cached tokens on top of the input count",
			call: Call{Model: "claude-x", Usage: &Usage{
				InputTokens: 1000, OutputTokens: 500, ThinkingTokens: 200, CachedTokens: 300,
			}},
			wantStatus: StatusPriced,
			// 1000*3 + 500*15 + 300*0.3, per million
			wantCost:  0.003 + 0.0075 + 0.00009,
			wantKnown: true,
		},
		{
			// OpenAI's prompt total already contains the cached tokens, so
			// charging both in full bills them twice.
			name: "openai charges cached tokens out of the input count",
			call: Call{Model: "gpt-x", Usage: &Usage{
				InputTokens: 1000, OutputTokens: 500, ThinkingTokens: 200, CachedTokens: 300,
			}},
			wantStatus: StatusPriced,
			// (1000-300)*2.5 + 500*10 + 300*1.25, per million
			wantCost:  0.00175 + 0.005 + 0.000375,
			wantKnown: true,
		},
		{
			name: "gemini follows the same inclusive convention as openai",
			call: Call{Model: "gemini-x", Usage: &Usage{
				InputTokens: 1000, OutputTokens: 500, CachedTokens: 300,
			}},
			wantStatus: StatusPriced,
			// (1000-300)*0.3 + 500*2.5 + 300*0.075, per million
			wantCost:  0.00021 + 0.00125 + 0.0000225,
			wantKnown: true,
		},
		{
			name: "a provider we have never seen is assumed to be inclusive",
			call: Call{Model: "acme-x", Usage: &Usage{
				InputTokens: 1000, OutputTokens: 500, CachedTokens: 300,
			}},
			wantStatus: StatusPriced,
			// (1000-300)*1 + 500*2 + 300*0.1, per million
			wantCost:  0.0007 + 0.001 + 0.00003,
			wantKnown: true,
		},
		{
			name: "no published cache rate charges cached tokens as ordinary input, and says so",
			call: Call{Model: "gpt-nocache", Usage: &Usage{
				InputTokens: 1000, OutputTokens: 500, CachedTokens: 300,
			}},
			wantStatus: StatusPricedPartial,
			// (1000-300)*2 + 500*4 + 300*2, per million
			wantCost:  0.0014 + 0.002 + 0.0006,
			wantKnown: true,
		},
		{
			name: "anthropic with no published cache rate is partial too",
			call: Call{Model: "claude-nocache", Usage: &Usage{
				InputTokens: 1000, OutputTokens: 500, CachedTokens: 300,
			}},
			wantStatus: StatusPricedPartial,
			// 1000*3 + 500*15 + 300*3, per million
			wantCost:  0.003 + 0.0075 + 0.0009,
			wantKnown: true,
		},
		{
			// Nothing was charged at a guessed rate, so there is nothing partial
			// about the answer even though the rate publishes no cache price.
			name: "no cache rate is not partial when nothing came from cache",
			call: Call{Model: "gpt-nocache", Usage: &Usage{
				InputTokens: 1000, OutputTokens: 500,
			}},
			wantStatus: StatusPriced,
			wantCost:   0.002 + 0.002,
			wantKnown:  true,
		},
		{
			// A record claiming more cache reads than prompt tokens is reporting
			// something the arithmetic cannot mean; it must not go negative.
			name: "more cached tokens than input tokens cannot produce a negative charge",
			call: Call{Model: "gpt-x", Usage: &Usage{
				InputTokens: 100, OutputTokens: 0, CachedTokens: 500,
			}},
			wantStatus: StatusPriced,
			// billable input floors at 0; 500*1.25 per million
			wantCost:  0.000625,
			wantKnown: true,
		},
		{
			name:       "an embedding is priced from its input alone",
			call:       Call{Model: "text-embedding-x", Embedding: true, Usage: &Usage{InputTokens: 10000}},
			wantStatus: StatusPriced,
			wantCost:   0.0002,
			wantKnown:  true,
		},
		{
			// The runtime reports neither for an embedding, so anything found in
			// those fields is not accounting the provider sent.
			name: "an embedding ignores output and cache counts it should never carry",
			call: Call{Model: "text-embedding-x", Embedding: true, Usage: &Usage{
				InputTokens: 10000, OutputTokens: 9999, CachedTokens: 9999,
			}},
			wantStatus: StatusPriced,
			wantCost:   0.0002,
			wantKnown:  true,
		},
		{
			name:       "a provider that reported no usage has no cost to know",
			call:       Call{Model: "gpt-x", Usage: nil},
			wantStatus: StatusNoUsage,
			wantKnown:  false,
		},
		{
			name:       "a model the card does not cover has an unknown cost, not a zero one",
			call:       Call{Model: "model-nobody-published", Usage: &Usage{InputTokens: 1000, OutputTokens: 500}},
			wantStatus: StatusUnpricedModel,
			wantKnown:  false,
		},
		{
			name:       "malformed negative counts cannot produce a negative charge",
			call:       Call{Model: "gpt-x", Usage: &Usage{InputTokens: -5, OutputTokens: -3, CachedTokens: -1}},
			wantStatus: StatusPriced,
			wantCost:   0,
			wantKnown:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := priceCard().Price(tc.call)

			if got.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tc.wantStatus)
			}
			cost, known := got.CostUSD()
			if known != tc.wantKnown {
				t.Fatalf("cost known = %v, want %v", known, tc.wantKnown)
			}
			if !known {
				if cost != 0 {
					t.Fatalf("an unknown cost reported %v, want 0", cost)
				}
				return
			}
			if math.Abs(cost-tc.wantCost) > epsilon {
				t.Fatalf("cost = %.12f, want %.12f", cost, tc.wantCost)
			}
		})
	}
}

// The connectors normalize every provider to an output count that already
// contains its reasoning tokens, so reading ThinkingTokens anywhere in the
// arithmetic bills reasoning twice.
func TestPriceIgnoresThinkingTokens(t *testing.T) {
	card := priceCard()
	without := card.Price(Call{Model: "claude-x", Usage: &Usage{InputTokens: 1000, OutputTokens: 500}})
	with := card.Price(Call{Model: "claude-x", Usage: &Usage{
		InputTokens: 1000, OutputTokens: 500, ThinkingTokens: 400,
	}})

	a, _ := without.CostUSD()
	b, _ := with.CostUSD()
	if math.Abs(a-b) > epsilon {
		t.Fatalf("reasoning tokens changed the cost: %.12f with, %.12f without", b, a)
	}
}

// The rate that produced a cost is recorded on the result so a number frozen
// months ago can still be traced back to it.
func TestPriceReportsTheRateThatProducedIt(t *testing.T) {
	got := priceCard().Price(Call{Model: "gpt-x", Usage: &Usage{InputTokens: 1000}})

	if got.PriceID != "openai" {
		t.Fatalf("price id = %q, want %q", got.PriceID, "openai")
	}
	if got.Provider != "OPENAI" {
		t.Fatalf("provider = %q, want %q", got.Provider, "OPENAI")
	}
}

// Nothing priced the call, so nothing may claim to have.
func TestUnpricedCallNamesNoRate(t *testing.T) {
	for _, call := range []Call{
		{Model: "model-nobody-published", Usage: &Usage{InputTokens: 1000}},
		{Model: "gpt-x", Usage: nil},
	} {
		got := priceCard().Price(call)
		if got.PriceID != "" || got.Provider != "" {
			t.Fatalf("%+v named rate %q from %q, want neither", call, got.PriceID, got.Provider)
		}
	}
}

// A refresher holds no card until its first load lands. Calls in that window are
// recorded as unpriced rather than dropped or priced at zero.
func TestNilTablePricesNothing(t *testing.T) {
	var card *Table
	got := card.Price(Call{Model: "gpt-x", Usage: &Usage{InputTokens: 1000, OutputTokens: 500}})

	if got.Status != StatusUnpricedModel {
		t.Fatalf("status = %q, want %q", got.Status, StatusUnpricedModel)
	}
	if _, known := got.CostUSD(); known {
		t.Fatal("a nil card produced a known cost")
	}
}

// TestPriceUsesTheReportedProviderNotTheMatchedRate is the point of #254.
//
// A bare `claude-*` pattern is published under BEDROCK and AWS as well as
// ANTHROPIC, so an Anthropic model shipped before the catalogue lists it under
// its own dated entry resolves to one of those instead. The rate is close enough
// to be plausible; the *convention* is not. Anthropic reports an input count
// that excludes cached tokens, so applying the inclusive rule subtracts them
// from a total that never contained them — silently under-charging by the whole
// cached amount.
func TestPriceUsesTheReportedProviderNotTheMatchedRate(t *testing.T) {
	// One model, one rate, published under a provider that is not the vendor.
	table := NewTable([]Rate{{
		ID: "bedrock-claude", Provider: "BEDROCK", Pattern: "claude", Operator: OpIncludes,
		InputPer1M: 3, OutputPer1M: 15, CacheReadPer1M: ptr(0.3),
	}})

	usage := &Usage{InputTokens: 1000, OutputTokens: 500, CachedTokens: 900}
	call := Call{Model: "claude-brand-new-20991231", Usage: usage}

	// Without a reported provider the rate's BEDROCK wins and the inclusive rule
	// applies: 100 billable input tokens. This is the old behaviour, kept for
	// records stored before the runtime carried a provider.
	inferred := table.Price(call)
	wantInferred := amount(100, 3) + amount(500, 15) + amount(900, 0.3)
	if got, _ := inferred.CostUSD(); math.Abs(got-wantInferred) > epsilon {
		t.Errorf("inferred provider: cost = %.12f, want %.12f", got, wantInferred)
	}

	// With the runtime's own answer, the exclusive rule applies: all 1000 input
	// tokens are billable, because Anthropic never counted the cached ones there.
	call.Provider = "ANTHROPIC"
	reported := table.Price(call)
	wantReported := amount(1000, 3) + amount(500, 15) + amount(900, 0.3)
	if got, _ := reported.CostUSD(); math.Abs(got-wantReported) > epsilon {
		t.Errorf("reported provider: cost = %.12f, want %.12f", got, wantReported)
	}

	// And the difference is the whole under-charge the issue describes.
	if wantReported <= wantInferred {
		t.Fatal("fixture does not reproduce the under-charge")
	}

	// The stored Provider stays the rate's: it is the audit trail for which rate
	// produced the number, and conflating it with the reported family would lose
	// the ability to see that the two disagreed.
	if reported.Provider != "BEDROCK" {
		t.Errorf("Priced.Provider = %q, want the rate's BEDROCK", reported.Provider)
	}
}

// The reported family is normalized like every other provider string, so a
// connector spelling it in lower case still matches.
func TestPriceNormalizesTheReportedProvider(t *testing.T) {
	table := NewTable([]Rate{{
		ID: "bedrock-claude", Provider: "BEDROCK", Pattern: "claude", Operator: OpIncludes,
		InputPer1M: 3, OutputPer1M: 15, CacheReadPer1M: ptr(0.3),
	}})
	call := Call{
		Model:    "claude-x",
		Provider: " anthropic ",
		Usage:    &Usage{InputTokens: 1000, OutputTokens: 0, CachedTokens: 900},
	}
	want := amount(1000, 3) + amount(900, 0.3) // exclusive, i.e. recognised as Anthropic
	if got, _ := table.Price(call).CostUSD(); math.Abs(got-want) > epsilon {
		t.Errorf("cost = %.12f, want %.12f (provider should normalize)", got, want)
	}
}
