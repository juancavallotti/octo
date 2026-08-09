package cost

// tokensPerUnit is the denominator every published rate is quoted against: the
// catalogues price per one million tokens.
const tokensPerUnit = 1_000_000

// The outcomes of pricing one model call. Every stored cost carries one of these
// beside it, because the number alone cannot say whether it is a cost, an
// estimate, or an absence — and an absence read as zero is the failure mode this
// whole package exists to avoid.
const (
	// StatusPriced is a cost computed in full from a known rate.
	StatusPriced Status = "priced"
	// StatusPricedPartial is a cost computed from a rate that published nothing
	// for cache reads, so cached tokens were charged at the full input rate. It
	// over-states rather than under-states, and says which.
	StatusPricedPartial Status = "priced_partial"
	// StatusUnpricedModel is a call whose model no rate in the card matches. The
	// cost is unknown — not zero.
	StatusUnpricedModel Status = "unpriced_model"
	// StatusNoUsage is a call whose provider reported no token usage at all.
	// There is nothing to price, which is different from pricing it at nothing.
	StatusNoUsage Status = "no_usage"
)

// anthropicProvider is the one vendor whose reported input count EXCLUDES the
// tokens served from cache; see Table.Price for why that decides the arithmetic.
const anthropicProvider = "ANTHROPIC"

// Status says what a stored cost means.
type Status string

// Usage is the token accounting a provider reported for one model call, as the
// runtime normalizes it.
//
// OutputTokens is the billing-authoritative total and ALREADY INCLUDES
// ThinkingTokens: the LLM connectors normalize every provider to that inclusive
// figure precisely so a consumer never has to know which one answered. Adding the
// two together therefore bills reasoning twice, which is why nothing here reads
// ThinkingTokens at all — it is carried for reporting, not for arithmetic.
//
// CachedTokens is cache-READ tokens in all three connectors. Cache WRITES are not
// captured anywhere in the runtime today, so a call that populated a cache is
// under-costed by whatever the write cost; that is a gap in the source data, not
// one this package can close.
type Usage struct {
	InputTokens    int
	OutputTokens   int
	ThinkingTokens int
	CachedTokens   int
}

// Call is one model call as a trace record reports it.
type Call struct {
	// Model is the model that actually served the call, which is not necessarily
	// the one that was asked for.
	Model string
	// Usage is what the provider reported, and is nil when it reported nothing.
	// Nil is not an empty Usage: a provider staying silent and a provider
	// charging zero are different facts, and only one of them is a cost.
	Usage *Usage
	// Embedding marks an llm.embed rather than an llm.turn. The two are billed
	// from different rate cards and share almost no accounting — an embedding has
	// no output tokens and no cache — so they cannot be priced by one path.
	Embedding bool
}

// Priced is what one call cost, and how much that number can be trusted.
//
// The cost is deliberately unexported and reachable only through CostUSD, which
// makes the caller acknowledge that it may not exist. A plain float64 field would
// read as zero for an unknown model, and a zero that means "we could not price
// this" is indistinguishable from a zero that means "this was free" in every
// total built downstream of it.
type Priced struct {
	Status Status
	// Provider and PriceID identify the rate that produced the cost, and are
	// empty when nothing priced it. PriceID is stored on the record so a cost
	// frozen months ago can still be traced to the rate that produced it.
	Provider string
	PriceID  string

	cost float64
}

// CostUSD returns what the call cost and whether that is known at all. A false
// second return must be stored as NULL, never as zero.
func (p Priced) CostUSD() (float64, bool) {
	switch p.Status {
	case StatusPriced, StatusPricedPartial:
		return p.cost, true
	default:
		return 0, false
	}
}

// Price resolves the rate for a call and applies it.
//
// The cached-token arithmetic is the part that is easy to get quietly wrong,
// because providers disagree about what their input count contains. Anthropic
// reports the uncached remainder, so its cached tokens are additional; OpenAI and
// Gemini report a total that already contains them, so charging both the full
// input count and the cache count bills the cached tokens twice. The rule is
// therefore keyed on the provider the rate resolved to — which is the reason
// resolution bothers to disambiguate providers at all — with the inclusive
// convention as the default, since it is what the OpenAI-compatible APIs follow.
func (t *Table) Price(call Call) Priced {
	if call.Usage == nil {
		return Priced{Status: StatusNoUsage}
	}
	rate, found := t.Rate(call.Model)
	if !found {
		return Priced{Status: StatusUnpricedModel}
	}

	priced := Priced{Status: StatusPriced, Provider: rate.Provider, PriceID: rate.ID}

	// An embedding is priced from its input alone. The runtime reports nothing
	// else for one — no output, no reasoning, no cache — so reading those fields
	// here would be inventing accounting the provider never sent.
	if call.Embedding {
		priced.cost = amount(nonNegative(call.Usage.InputTokens), rate.InputPer1M)
		return priced
	}

	input := nonNegative(call.Usage.InputTokens)
	output := nonNegative(call.Usage.OutputTokens)
	cached := nonNegative(call.Usage.CachedTokens)

	billableInput := input
	if rate.Provider != anthropicProvider {
		// Clamped rather than trusted: a provider reporting more cached tokens
		// than input tokens is reporting something this arithmetic cannot mean,
		// and a negative charge is worse than a conservative one.
		billableInput = nonNegative(input - cached)
	}

	priced.cost = amount(billableInput, rate.InputPer1M) + amount(output, rate.OutputPer1M)

	if cached == 0 {
		return priced
	}
	if rate.CacheReadPer1M == nil {
		// No published cache rate. Charging the tokens at the full input rate
		// over-states the cost; charging them at nothing under-states it while
		// looking exactly like a confident answer. Over-state, and say so.
		priced.Status = StatusPricedPartial
		priced.cost += amount(cached, rate.InputPer1M)
		return priced
	}
	priced.cost += amount(cached, *rate.CacheReadPer1M)
	return priced
}

// amount converts a token count and a per-million rate into money.
func amount(tokens int, per1M float64) float64 {
	return float64(tokens) / tokensPerUnit * per1M
}

// nonNegative floors a reported count at zero, so a malformed record cannot
// produce a negative charge.
func nonNegative(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
