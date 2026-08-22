package cost

// Pricer prices a model call against an ordered list of rate cards, taking the
// first answer that is one.
//
// Two sources rather than one because they cover different ground. OpenRouter's
// card is fresher and priced per model by the platform that sells them, but it
// only lists what OpenRouter itself routes to; helicone's carries the patterns
// OpenRouter never publishes — Bedrock, Azure, vendor-hosted ids. Preferring the
// first and falling through to the second is what makes a model priced by
// whichever card actually knows it, rather than unpriced because the preferred
// card had not heard of it.
//
// The order is the caller's and is not sorted here: "preferred" is a deployment
// decision, and a pricer that reordered its own sources would make the config
// that set them a suggestion.
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
// Only StatusUnpricedModel falls through to the next card — that status means
// "this card has never heard of this model", which is exactly the question the
// next card might answer. StatusNoUsage does not: a provider that reported no
// tokens leaves nothing for any card to price, so asking a second one would
// return the same answer more slowly. A partial pricing does not either: it is a
// real cost from a real rate, and preferring a different card's guess over the
// preferred card's arithmetic would make the order mean something else.
//
// A pricer with no sources — or a nil one — prices nothing, which is what a
// service that has not loaded a card yet must report. Every Table already
// degrades that way; this keeps the property.
func (p *Pricer) Price(call Call) Priced {
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
