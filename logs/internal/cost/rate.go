// Package cost prices the model calls a runtime reports.
//
// A trace record for an llm.turn or llm.embed carries the model that served the
// call and the tokens the provider charged: the runtime deliberately knows no
// rate card, so pricing is the consumer's job. This package is that rate card
// and the arithmetic over it.
//
// One record in ten thousand carries money anyway, and it is not an exception to
// that rule. Some providers report what they charged — OpenRouter does — and the
// runtime relays the figure without computing it. Such a call is priced by
// reportedCost in price.go and never reaches a table here at all.
//
// The rate card is keyed by patterns rather than model ids, because that is how
// the upstream catalogues publish it: `claude-3-5-sonnet-20241022` under equals
// names one model, `gemini-2.0-flash` under includes names a family whose exact
// ids nobody enumerates. Resolving a served model id therefore means picking the
// most specific pattern that applies, which is what Table does.
package cost

import (
	"math"
	"sort"
	"strings"
)

// The operators a rate card entry's pattern is matched with. The vocabulary is
// the upstream catalogue's, kept verbatim rather than translated so an entry can
// be read against the published feed without a lookup table in between.
const (
	// OpEquals matches a model id exactly.
	OpEquals Operator = "equals"
	// OpStartsWith matches every model id with the pattern as its prefix.
	OpStartsWith Operator = "startsWith"
	// OpIncludes matches every model id containing the pattern anywhere.
	OpIncludes Operator = "includes"
)

// rankedProviders orders the vendors this platform can actually call, most
// preferred first, and exists to settle a genuine ambiguity: a model id can be
// published under several providers at different prices — `gpt-4o` appears under
// both OPENAI and AZURE — and the trace record cannot break the tie, because the
// attribute it carries is the connector's *authored instance name*, not its
// vendor. Preferring the providers there are connectors for
// (runtime/connectors/llm/) is the answer that matches what actually served the
// call. Anything not listed sorts after these, alphabetically.
var rankedProviders = []string{"ANTHROPIC", "OPENAI", "GOOGLE"}

// Operator says how a Rate's pattern is matched against a served model id.
type Operator string

// Rate is one entry of the rate card: a pattern matching model ids, the provider
// publishing it, and what it charges per million tokens.
type Rate struct {
	// ID is the llm_prices row this came from. It is recorded on every record the
	// rate prices, so a cost frozen months ago can still be traced back to the
	// exact rate that produced it. Empty for a rate not yet persisted.
	ID string

	// Provider is the vendor, upper-cased as the catalogue publishes it. It is
	// not decoration: it decides how cached tokens are charged, because providers
	// disagree on whether the input count already includes them.
	Provider string

	// Pattern is matched against a served model id under Operator.
	Pattern  string
	Operator Operator

	InputPer1M  float64
	OutputPer1M float64

	// CacheReadPer1M and CacheWritePer1M are nil when the catalogue publishes no
	// cache rate for this model. Nil is a different fact from a rate of zero, and
	// the difference decides whether cached tokens can be priced at all rather
	// than merely how much they cost.
	CacheReadPer1M  *float64
	CacheWritePer1M *float64

	// Preferred marks an entry the catalogue itself flags as the current public
	// one. It settles the duplicates the feed genuinely carries: the same
	// provider, pattern and operator published twice at different prices.
	Preferred bool
}

// matches reports whether this entry's pattern applies to a normalized model id.
// An unrecognized operator matches nothing: an entry whose rule cannot be applied
// must price nothing rather than price everything.
func (r Rate) matches(model string) bool {
	switch r.Operator {
	case OpEquals:
		return model == r.Pattern
	case OpStartsWith:
		return strings.HasPrefix(model, r.Pattern)
	case OpIncludes:
		return strings.Contains(model, r.Pattern)
	default:
		return false
	}
}

// usable reports whether the entry can be admitted to a table at all.
//
// An empty pattern is rejected rather than kept, because under includes it
// matches every model id there is: one such row in the feed would silently price
// the entire catalogue at whatever it charges, and every total built on it would
// be wrong in a way no arithmetic reveals. A rate whose figures are not prices
// is rejected on the same grounds — see usablePrice.
//
// The two cache halves are deliberately not checked here. They are optional, and
// a rate with a malformed cache price can still price its call correctly at
// priced_partial, so refusing the whole entry would lose a model over the half
// of it that was fine. A decoder that can produce one nils it instead.
func (r Rate) usable() bool {
	if r.Pattern == "" {
		return false
	}
	if !usablePrice(r.InputPer1M) || !usablePrice(r.OutputPer1M) {
		return false
	}
	switch r.Operator {
	case OpEquals, OpStartsWith, OpIncludes:
		return true
	default:
		return false
	}
}

// usablePrice reports whether a figure is a price at all: a real, non-negative
// quantity.
//
// Neither half of that is theoretical. A NaN or an infinity multiplies through
// the arithmetic into cost_usd, where one poisoned row makes every SUM over it
// meaningless — and unlike an unpriced call, nothing in the stored record says
// so. A negative rate is the failure this package already refuses twice
// elsewhere: reportedCost rejects a negative figure a provider reports, and
// nonNegative floors a reported token count, both because a credit recorded as a
// cost nets silently against real charges.
//
// It is a property of the number rather than of any one feed, which is why it
// lives here rather than in a decoder: OpenRouter's "-1" for a variable price is
// one instance of the rule, not a case in it.
func usablePrice(per1M float64) bool {
	return !math.IsNaN(per1M) && !math.IsInf(per1M, 0) && per1M >= 0
}

// normalize is how both sides of a match are spelled: lower-cased and trimmed, so
// a pattern and a model id that differ only in case or stray whitespace are the
// same string by the time they meet.
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// normalizeProvider is how a vendor name is spelled: upper-cased and trimmed, as
// the catalogues publish it. It is a separate spelling from normalize because
// providers are compared against rankedProviders and stored as an identity, not
// matched against anything.
func normalizeProvider(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// moreSpecific orders two entries by how precisely they name a model, most
// precise first, and is a *total* order on distinct entries — that is the point
// of its length. A partial order would leave ties for the sort to break by input
// position, and the input is a map iteration and an HTTP response, so the same
// catalogue would price the same call differently on different runs.
//
// The keys, in order:
//
//  1. Operator specificity. An exact name beats a prefix beats a substring.
//  2. Pattern length, longest first. Required rather than cosmetic:
//     `gemini-2.5-flash-lite-preview-06-17` contains both `gemini-2.5-flash` and
//     `gemini-2.5-flash-lite`, and only the longer one names what actually ran.
//  3. Provider preference, then provider name — see rankedProviders.
//  4. The catalogue's own "this is the current one" flag.
//  5. Cheapest input, then cheapest output. When the feed publishes the same
//     pattern twice with nothing left to tell them apart, the lower price is the
//     one that under-claims rather than over-claims.
//  6. Pattern, then id, purely to close the order.
func moreSpecific(a, b Rate) bool {
	if x, y := operatorRank(a.Operator), operatorRank(b.Operator); x != y {
		return x < y
	}
	if len(a.Pattern) != len(b.Pattern) {
		return len(a.Pattern) > len(b.Pattern)
	}
	if x, y := providerRank(a.Provider), providerRank(b.Provider); x != y {
		return x < y
	}
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	if a.Preferred != b.Preferred {
		return a.Preferred
	}
	if a.InputPer1M != b.InputPer1M {
		return a.InputPer1M < b.InputPer1M
	}
	if a.OutputPer1M != b.OutputPer1M {
		return a.OutputPer1M < b.OutputPer1M
	}
	if a.Pattern != b.Pattern {
		return a.Pattern < b.Pattern
	}
	return a.ID < b.ID
}

// operatorRank scores an operator by specificity, lowest being most specific.
func operatorRank(op Operator) int {
	switch op {
	case OpEquals:
		return 0
	case OpStartsWith:
		return 1
	case OpIncludes:
		return 2
	default:
		return 3
	}
}

// providerRank scores a provider by rankedProviders, with everything unlisted
// sharing the last rank so moreSpecific falls through to comparing names.
func providerRank(provider string) int {
	for i, name := range rankedProviders {
		if provider == name {
			return i
		}
	}
	return len(rankedProviders)
}

// sortBySpecificity puts entries in resolution order, so a lookup is a scan that
// stops at its first match rather than a search for the best one.
func sortBySpecificity(rates []Rate) {
	sort.SliceStable(rates, func(i, j int) bool { return moreSpecific(rates[i], rates[j]) })
}
