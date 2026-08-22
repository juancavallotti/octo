package cost

import "testing"

// cardOf builds a refresher already holding a card, without a store or a feed.
// The fallback behaviour under test is about which card answers, not about how
// either got there.
func cardOf(t *testing.T, rates ...Rate) *Refresher {
	t.Helper()
	r := &Refresher{}
	r.publish(rates)
	return r
}

func usage(input, output int) *Usage {
	return &Usage{InputTokens: input, OutputTokens: output}
}

func TestPricerTakesTheFirstCardThatKnowsTheModel(t *testing.T) {
	preferred := cardOf(t, Rate{
		ID: "pref-1", Provider: "ANTHROPIC", Pattern: "claude-sonnet-4-5",
		Operator: OpStartsWith, InputPer1M: 3, OutputPer1M: 15,
	})
	fallback := cardOf(t, Rate{
		ID: "fall-1", Provider: "ANTHROPIC", Pattern: "claude-sonnet-4-5",
		Operator: OpStartsWith, InputPer1M: 99, OutputPer1M: 99,
	})

	got := NewPricer(preferred, fallback).Price(Call{
		Model: "claude-sonnet-4-5-20250929", Provider: "ANTHROPIC", Usage: usage(1_000_000, 0),
	})
	if got.Status != StatusPriced {
		t.Fatalf("status = %q, want priced", got.Status)
	}
	if got.PriceID != "pref-1" {
		t.Errorf("price id = %q, want the preferred card's rate", got.PriceID)
	}
	if amount, _ := got.CostUSD(); amount != 3 {
		t.Errorf("cost = %v, want the preferred card's 3", amount)
	}
}

// The whole reason for a second card: a model the preferred one has never heard
// of is priced rather than lost. OpenRouter lists what OpenRouter routes to;
// Bedrock and Azure ids are not on it.
func TestPricerFallsThroughForAModelTheFirstCardDoesNotKnow(t *testing.T) {
	preferred := cardOf(t, Rate{
		ID: "pref-1", Provider: "OPENAI", Pattern: "gpt-4o",
		Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10,
	})
	fallback := cardOf(t, Rate{
		ID: "fall-1", Provider: "BEDROCK", Pattern: "anthropic.claude-3-5-sonnet",
		Operator: OpStartsWith, InputPer1M: 3, OutputPer1M: 15,
	})

	got := NewPricer(preferred, fallback).Price(Call{
		Model: "anthropic.claude-3-5-sonnet-20241022-v2:0", Usage: usage(1_000_000, 0),
	})
	if got.Status != StatusPriced || got.PriceID != "fall-1" {
		t.Errorf("priced = %+v, want the fallback card's rate", got)
	}
}

func TestPricerReportsUnpricedWhenNoCardKnowsTheModel(t *testing.T) {
	got := NewPricer(cardOf(t), cardOf(t)).Price(Call{
		Model: "some-model-nobody-lists", Usage: usage(10, 2),
	})
	if got.Status != StatusUnpricedModel {
		t.Errorf("status = %q, want unpriced_model", got.Status)
	}
	if _, known := got.CostUSD(); known {
		t.Error("an unpriced call must not report a cost")
	}
}

// no_usage is not a question a second card can answer differently: the provider
// reported no tokens, so there is nothing for any rate to multiply.
func TestPricerDoesNotFallThroughOnNoUsage(t *testing.T) {
	fallback := cardOf(t, Rate{
		ID: "fall-1", Provider: "OPENAI", Pattern: "gpt-4o",
		Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10,
	})

	got := NewPricer(cardOf(t), fallback).Price(Call{Model: "gpt-4o"})
	if got.Status != StatusNoUsage {
		t.Errorf("status = %q, want no_usage", got.Status)
	}
}

// A partial pricing is a real cost from a real rate. Falling through would make
// the preferred card's arithmetic lose to a second card's guess, which is not
// what "preferred" means.
func TestPricerKeepsAPartialPricingFromThePreferredCard(t *testing.T) {
	cacheRead := 0.3
	preferred := cardOf(t, Rate{
		ID: "pref-1", Provider: "OPENAI", Pattern: "gpt-4o",
		Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10,
	})
	fallback := cardOf(t, Rate{
		ID: "fall-1", Provider: "OPENAI", Pattern: "gpt-4o",
		Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10, CacheReadPer1M: &cacheRead,
	})

	got := NewPricer(preferred, fallback).Price(Call{
		Model:    "gpt-4o",
		Provider: "OPENAI",
		Usage:    &Usage{InputTokens: 1_000, OutputTokens: 10, CachedTokens: 500},
	})
	if got.Status != StatusPricedPartial {
		t.Fatalf("status = %q, want priced_partial", got.Status)
	}
	if got.PriceID != "pref-1" {
		t.Errorf("price id = %q, want the preferred card's rate", got.PriceID)
	}
}

// A service that has not loaded a card yet must report unpriced rather than
// panic, which is the property every Table already guarantees.
func TestPricerWithoutSourcesPricesNothing(t *testing.T) {
	call := Call{Model: "gpt-4o", Usage: usage(10, 2)}

	for name, pricer := range map[string]*Pricer{
		"nil":         nil,
		"no sources":  NewPricer(),
		"empty cards": NewPricer(&Refresher{}),
	} {
		t.Run(name, func(t *testing.T) {
			if got := pricer.Price(call); got.Status != StatusUnpricedModel {
				t.Errorf("status = %q, want unpriced_model", got.Status)
			}
			if pricer.Len() != 0 {
				t.Errorf("len = %d, want 0", pricer.Len())
			}
		})
	}
}

func TestPricerLenCountsEveryCard(t *testing.T) {
	a := cardOf(t, Rate{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 1})
	b := cardOf(t,
		Rate{Provider: "ANTHROPIC", Pattern: "claude-3", Operator: OpStartsWith, InputPer1M: 1},
		Rate{Provider: "GOOGLE", Pattern: "gemini", Operator: OpIncludes, InputPer1M: 1},
	)
	if got := NewPricer(a, b).Len(); got != 3 {
		t.Errorf("len = %d, want 3", got)
	}
}

// The consumer holds its pricer behind a one-method interface; this is the
// compile-time check that a Pricer still satisfies what NewTraceConsumer takes.
func TestPricerSatisfiesThePriceCall(t *testing.T) {
	var p interface{ Price(Call) Priced } = NewPricer()
	if got := p.Price(Call{}); got.Status != StatusNoUsage {
		t.Errorf("status = %q, want no_usage for a call with no usage", got.Status)
	}
}
