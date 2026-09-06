package cost

import "math"

// priceScale is the number of decimal places the stored rate columns keep
// (numeric(20,10)). Incoming rates are rounded to it before they are compared or
// written, so what a diff compares is what a store can hold.
//
// Without that, a published rate carrying more precision than the column would
// differ from its own stored copy on every single refresh: each one would close a
// row and open an identical one, growing the history without bound and churning
// the price id recorded on every record priced in between. The feed publishes
// nothing that fine today, which is exactly why the failure would go unnoticed.
const priceScale = 10

// Change is one difference a fetched card makes to the stored one.
type Change struct {
	// Rate is the incoming rate to open, quantized to the stored scale.
	Rate Rate
	// Supersedes is the id of the open row this replaces, and is empty when the
	// rate is new. A change is always a close plus an insert rather than an
	// update, because a cost frozen onto a record months ago must still resolve
	// to the rate that produced it.
	Supersedes string
}

// Diff reports what a fetched card changes about the stored one.
//
// A pattern the fetch no longer carries is deliberately absent from the result:
// it stays open. A model vanishing from a catalogue is a fact about the
// catalogue, not about the model, and closing the row would unprice calls the
// rate already explains — including calls yet to be ingested from a runtime that
// is still using it.
func Diff(stored, fetched []Rate) []Change {
	byKey := make(map[rateKey]Rate, len(stored))
	for _, rate := range stored {
		byKey[keyOf(rate)] = rate
	}

	// fetched arrives in resolution order and is walked in that order, so the
	// same fetch always produces the same changes in the same sequence.
	var changes []Change
	for _, incoming := range fetched {
		incoming = quantizeRate(incoming)
		current, known := byKey[keyOf(incoming)]
		if !known {
			changes = append(changes, Change{Rate: incoming})
			continue
		}
		if samePrice(current, incoming) {
			continue
		}
		changes = append(changes, Change{Rate: incoming, Supersedes: current.ID})
	}
	return changes
}

// rateKey identifies a rate card entry across refreshes. It is the same triple
// the stored card is uniquely indexed on, so a diff and the database agree about
// what counts as "the same entry".
type rateKey struct {
	provider string
	pattern  string
	operator Operator
}

func keyOf(r Rate) rateKey {
	return rateKey{provider: r.Provider, pattern: r.Pattern, operator: r.Operator}
}

// samePrice reports whether two entries for one key charge the same thing. The id
// and the key itself are excluded: what makes a change is the price, not which
// row happens to be carrying it.
func samePrice(a, b Rate) bool {
	return a.InputPer1M == b.InputPer1M &&
		a.OutputPer1M == b.OutputPer1M &&
		a.Preferred == b.Preferred &&
		sameOptional(a.CacheReadPer1M, b.CacheReadPer1M) &&
		sameOptional(a.CacheWritePer1M, b.CacheWritePer1M)
}

// sameOptional compares two optional rates by value. A rate going from absent to
// published is a change even when the published number is zero: pricing reads the
// two differently, so storing them the same would lose the distinction.
func sameOptional(a, b *float64) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

// quantizeRate rounds every rate on an entry to the stored scale.
func quantizeRate(r Rate) Rate {
	r.InputPer1M = quantize(r.InputPer1M)
	r.OutputPer1M = quantize(r.OutputPer1M)
	r.CacheReadPer1M = quantizeOptional(r.CacheReadPer1M)
	r.CacheWritePer1M = quantizeOptional(r.CacheWritePer1M)
	return r
}

func quantizeOptional(f *float64) *float64 {
	if f == nil {
		return nil
	}
	rounded := quantize(*f)
	return &rounded
}

// quantize rounds to priceScale decimal places. Published rates are small
// numbers, so scaling them stays far inside the range where a float64 holds an
// integer exactly.
func quantize(f float64) float64 {
	factor := math.Pow(10, priceScale)
	return math.Round(f*factor) / factor
}
