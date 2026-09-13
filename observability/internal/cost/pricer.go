package cost

// Pricer prices a model call against an ordered list of rate cards, taking the
// first answer that is one.
//
// Several sources rather than one because they cover different ground: one card
// is fresher but lists only what its own platform routes to, another carries the
// patterns the first never publishes. Falling through prices a model by whichever
// card actually knows it.
//
// The order is the caller's and is not sorted here.
type Pricer struct {
	sources []*Refresher
}

// NewPricer returns a pricer over cards in preference order.
func NewPricer(sources ...*Refresher) *Pricer {
	return &Pricer{sources: sources}
}

// Price prices one call. It is the whole of what the ingest path needs from this
// package.
//
// A cost the provider reported outranks every card, because it is not an
// estimate: it is what was charged, including the per-request and per-image
// portions no token count can reconstruct. Nothing is consulted after it.
//
// Only StatusUnpricedModel falls through to the next card, since that status means
// "this card has never heard of this model". StatusNoUsage leaves nothing for any
// card to price, and a partial pricing is a real cost from a real rate.
//
// A pricer with no sources — or a nil one — prices nothing, which is what a
// service that has not loaded a card yet must report.
func (p *Pricer) Price(call Call) Priced {
	if reported, ok := reportedCost(call); ok {
		return reported
	}
	// Answered before any card is consulted, and answered the same way when there
	// are none: whether a provider reported tokens is a fact about the call, not
	// about what anyone knows how to price.
	if call.Usage == nil {
		return Priced{Status: StatusNoUsage}
	}
	if p == nil {
		return Priced{Status: StatusUnpricedModel}
	}

	priced := Priced{Status: StatusUnpricedModel}
	for _, source := range p.sources {
		priced = source.Price(call)
		if priced.Status != StatusUnpricedModel {
			return priced
		}
	}
	return priced
}

// Len is how many rates the cards hold between them, for a caller to log.
func (p *Pricer) Len() int {
	if p == nil {
		return 0
	}
	var total int
	for _, source := range p.sources {
		total += source.Card().Len()
	}
	return total
}
