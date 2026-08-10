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
	// StatusPricedPartial is a cost computed from a rate that published no cache
	// rate, so those tokens were charged at the full input rate. The direction of
	// the error depends on which half was missing: a cache read costs less than
	// input, so the fallback over-states it; a cache write costs more, so the
	// fallback under-states it. Either way the figure is an estimate, and this is
	// how it says so.
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
// CachedTokens is cache-READ tokens and CacheWriteTokens is cache CREATION. The
// two bill in opposite directions from ordinary input — a read cheaper, a write
// dearer — so they are counted and priced separately rather than summed.
type Usage struct {
	InputTokens    int
	OutputTokens   int
	ThinkingTokens int
	CachedTokens   int
	// CacheWriteTokens is cache CREATION: the tokens a call spent populating a
	// prompt cache, which bill above the input rate (Anthropic charges roughly
	// 1.25x). Only Anthropic reports one.
	CacheWriteTokens int
}

// Call is one model call as a trace record reports it.
type Call struct {
	// Model is the model that actually served the call, which is not necessarily
	// the one that was asked for.
	Model string
	// Provider is the vendor family the runtime stamped on the call, in this
	// package's vocabulary (ANTHROPIC, OPENAI, GOOGLE). Empty for a record written
	// before the runtime carried one, or by a connector that reports none.
	Provider string
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
// input count and the cache count bills the cached tokens twice. The inclusive
// convention is the default, since it is what the OpenAI-compatible APIs follow.
//
// The rule is keyed on the provider the *call* reported, not on the provider of
// whichever rate happened to match the model. Those differ exactly when it
// matters: bare claude-* patterns are published under AWS and BEDROCK as well,
// so an Anthropic model shipped before the catalogue lists it under ANTHROPIC
// falls through to one of those — and the exclusive arithmetic its usage was
// reported under is then applied as though it were inclusive, subtracting cached
// tokens from an input count that never contained them. That under-charges, and
// nothing in the stored record says so.
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
	written := nonNegative(call.Usage.CacheWriteTokens)

	billableInput := input
	if providerOf(call, rate) != anthropicProvider {
		// Clamped rather than trusted: a provider reporting more cached tokens
		// than input tokens is reporting something this arithmetic cannot mean,
		// and a negative charge is worse than a conservative one.
		billableInput = nonNegative(input - cached - written)
	}

	priced.cost = amount(billableInput, rate.InputPer1M) + amount(output, rate.OutputPer1M)
	priced.cost += t.cacheCost(&priced, rate, cached, written)
	return priced
}

// cacheCost prices the two cache halves and downgrades the status when either
// has no published rate.
//
// Both fall back to the input rate, and the two fallbacks err in opposite
// directions: a read costs *less* than input, so charging it at input
// over-states; a write costs *more*, so charging it at input under-states. Only
// the read fallback can be described as conservative. Neither invents a
// multiplier — a 1.25x write rate is Anthropic's published figure, not a
// universal one, and pricing from a rate nobody published would be a confident
// answer with nothing behind it. The status is what says so, either way.
func (t *Table) cacheCost(priced *Priced, rate Rate, cached, written int) float64 {
	var cost float64
	if cached > 0 {
		if rate.CacheReadPer1M == nil {
			priced.Status = StatusPricedPartial
			cost += amount(cached, rate.InputPer1M)
		} else {
			cost += amount(cached, *rate.CacheReadPer1M)
		}
	}
	if written > 0 {
		if rate.CacheWritePer1M == nil {
			priced.Status = StatusPricedPartial
			cost += amount(written, rate.InputPer1M)
		} else {
			cost += amount(written, *rate.CacheWritePer1M)
		}
	}
	return cost
}

// providerOf is the vendor family to apply the cached-token rule for: what the
// runtime stamped on the call, falling back to the provider of the rate that
// matched.
//
// The fallback is what every record stored before the runtime carried a provider
// replays under, and what a third-party connector that reports none still gets.
// It is the old behaviour exactly, kept so that adding the attribute changed no
// historical figure.
func providerOf(call Call, rate Rate) string {
	// Normalized before the emptiness check, not after: a reported provider of
	// whitespace is non-empty as a string but names nothing, and taking it would
	// skip the fallback and quietly apply the default convention.
	if reported := normalizeProvider(call.Provider); reported != "" {
		return reported
	}
	return rate.Provider
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
