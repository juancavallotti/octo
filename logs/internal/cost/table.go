package cost

import "strings"

// Table resolves a served model id to the rate that prices it.
//
// It is immutable once built and safe for concurrent readers, which is what lets
// a refresh publish a whole new rate card by swapping one pointer instead of
// locking every lookup behind a mutex the ingest path would contend on.
type Table struct {
	// exact holds the equals entries keyed by their pattern, since an exact
	// lookup is the overwhelming majority of the catalogue and of the traffic.
	exact map[string][]Rate
	// fuzzy holds the prefix and substring entries in resolution order.
	fuzzy []Rate
	// count is how many entries were admitted, for the refresher to report.
	count int
}

// NewTable builds a table from a rate card, normalizing patterns and providers so
// a lookup never has to.
//
// Entries that could never match anything — an empty pattern, an operator this
// package does not implement — are skipped rather than kept, on the grounds that
// a rule which cannot be applied should price nothing. Whoever decoded the feed
// is the one positioned to report how many rows that was, so this stays quiet and
// counts what it kept.
func NewTable(rates []Rate) *Table {
	t := &Table{exact: make(map[string][]Rate)}
	for _, r := range rates {
		r.Provider = strings.ToUpper(strings.TrimSpace(r.Provider))
		r.Pattern = normalize(r.Pattern)
		if !r.usable() {
			continue
		}
		t.count++
		if r.Operator == OpEquals {
			t.exact[r.Pattern] = append(t.exact[r.Pattern], r)
			continue
		}
		t.fuzzy = append(t.fuzzy, r)
	}

	// Sorted once here rather than compared on every lookup: the catalogue is
	// four figures of entries and changes twice a day, while a lookup happens per
	// model call.
	sortBySpecificity(t.fuzzy)
	for _, group := range t.exact {
		sortBySpecificity(group)
	}
	return t
}

// Rate returns the rate that prices a served model id.
//
// The second return is false when nothing in the card matches, and callers must
// treat that as "unknown", never as free: a zero Rate would price the call at
// nothing and quietly remove it from every total it belongs to. A nil table —
// which is what a refresher holds before its first load — reports the same thing,
// so an unpriced startup degrades rather than panics.
func (t *Table) Rate(model string) (Rate, bool) {
	if t == nil {
		return Rate{}, false
	}
	model = normalize(model)
	if model == "" {
		return Rate{}, false
	}

	// An equals entry outranks every prefix and substring entry by construction,
	// since operator specificity is the first key moreSpecific compares. So an
	// exact hit is the answer and the fuzzy list need not be touched.
	if group := t.exact[model]; len(group) > 0 {
		return group[0], true
	}

	// Otherwise the first match in resolution order is the most specific one.
	for _, r := range t.fuzzy {
		if r.matches(model) {
			return r, true
		}
	}
	return Rate{}, false
}

// Len is how many entries the table admitted, for a refresher to log. A nil table
// holds none.
func (t *Table) Len() int {
	if t == nil {
		return 0
	}
	return t.count
}
