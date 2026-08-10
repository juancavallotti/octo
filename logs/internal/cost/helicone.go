package cost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// SourceHelicone names this feed on every row it produces, so a stored price
	// says where it came from and a second source can be added beside it without
	// the two becoming indistinguishable.
	SourceHelicone = "helicone"

	// DefaultCatalogueURL is the published rate card. It is fetched whole rather
	// than queried per model: the entire catalogue is a couple of hundred
	// kilobytes, while per-model queries would be one request per model seen.
	DefaultCatalogueURL = "https://www.helicone.ai/api/llm-costs"

	// defaultFetchTimeout bounds one fetch. The card is refreshed on a schedule
	// and the previous one stays in use on failure, so waiting longer buys
	// nothing.
	defaultFetchTimeout = 30 * time.Second

	// maxCatalogueBytes bounds the response body. The real card is ~210 KB; this
	// leaves two orders of magnitude of headroom while keeping a misbehaving or
	// hostile endpoint from being read into memory without limit.
	maxCatalogueBytes = 8 << 20
)

// errEmptyCatalogue rejects a card that prices nothing. A 200 carrying no usable
// rows is far more likely to be an upstream fault than a real state of the world,
// and treating it as real would report every model call as unpriced.
var errEmptyCatalogue = errors.New("cost: catalogue contains no usable rates")

// Catalogue reads a published rate card over HTTP.
type Catalogue struct {
	url    string
	client *http.Client
}

// NewCatalogue returns a catalogue reader. An empty url falls back to
// DefaultCatalogueURL and a nil client to one carrying defaultFetchTimeout, so
// the zero configuration is the working one and a test can replace either.
func NewCatalogue(url string, client *http.Client) *Catalogue {
	if url == "" {
		url = DefaultCatalogueURL
	}
	if client == nil {
		client = &http.Client{Timeout: defaultFetchTimeout}
	}
	return &Catalogue{url: url, client: client}
}

// Fetched is one read of a published rate card.
type Fetched struct {
	// Rates is the card, at most one entry per provider/pattern/operator.
	Rates []Rate
	// Unusable counts published rows this package cannot apply — an operator it
	// does not implement, a pattern that would match everything.
	Unusable int
	// Duplicates counts rows dropped because a better row shares their key.
	Duplicates int
}

// Fetch reads the card and reduces it to rates that can be stored and applied.
//
// The feed genuinely publishes the same provider/model/operator more than once,
// sometimes at different prices, so reducing to one row per key is not tidying:
// the stored card is uniquely indexed on that key, and an unreduced fetch would
// be rejected wholesale by the database rather than partially.
func (c *Catalogue) Fetch(ctx context.Context) (Fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return Fetched{}, fmt.Errorf("cost: build catalogue request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("cost: fetch catalogue %q: %w", c.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Fetched{}, fmt.Errorf("cost: fetch catalogue %q: status %s", c.url, resp.Status)
	}

	var payload cataloguePayload
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCatalogueBytes)).Decode(&payload); err != nil {
		return Fetched{}, fmt.Errorf("cost: decode catalogue %q: %w", c.url, err)
	}

	fetched := reduce(payload.Data)
	if len(fetched.Rates) == 0 {
		return Fetched{}, errEmptyCatalogue
	}
	return fetched, nil
}

// cataloguePayload is the feed's envelope. Only `data` is read: the `metadata`
// block is descriptive prose and a count, and trusting a self-reported count over
// the rows actually present would be believing the label instead of the contents.
type cataloguePayload struct {
	Data []catalogueRow `json:"data"`
}

// catalogueRow is one published entry.
//
// The feed carries keys this does not name — prompt_audio_per_1m,
// completion_audio_per_1m, per_image, per_call — because some models bill for
// things beyond text tokens. They are ignored rather than mapped: the runtime
// reports token counts and nothing else, so there is no quantity here to multiply
// them by. A model billed per call or per image is therefore under-costed by
// whatever that portion is, which is a limit of what the traces carry.
type catalogueRow struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Operator    Operator `json:"operator"`
	InputPer1M  float64  `json:"input_cost_per_1m"`
	OutputPer1M float64  `json:"output_cost_per_1m"`

	// Absent rather than zero when the model publishes no cache price, which is
	// why these are pointers all the way from the wire to the rate.
	CacheWritePer1M *float64 `json:"prompt_cache_write_per_1m"`
	CacheReadPer1M  *float64 `json:"prompt_cache_read_per_1m"`

	// Preferred is the feed's own marker for the entry it considers current, and
	// is what tells apart the duplicates it publishes at different prices.
	Preferred bool `json:"show_in_playground"`
}

// rate converts a published row into a rate. The id is left empty: identity
// belongs to the stored row, not to a fetch.
func (r catalogueRow) rate() Rate {
	return Rate{
		Provider:        r.Provider,
		Pattern:         r.Model,
		Operator:        r.Operator,
		InputPer1M:      r.InputPer1M,
		OutputPer1M:     r.OutputPer1M,
		CacheReadPer1M:  r.CacheReadPer1M,
		CacheWritePer1M: r.CacheWritePer1M,
		Preferred:       r.Preferred,
	}
}

// reduce normalizes published rows and keeps the best one per key.
//
// "Best" is moreSpecific, the same ordering resolution uses — deliberately one
// rule rather than two. Between rows sharing a key it falls through to the feed's
// current-price flag and then to the cheaper price, so the choice is deterministic
// and errs toward under-claiming.
func reduce(rows []catalogueRow) Fetched {
	type key struct {
		provider string
		pattern  string
		operator Operator
	}

	var out Fetched
	rates := make([]Rate, 0, len(rows))
	for _, row := range rows {
		rate := row.rate()
		rate.Provider = normalizeProvider(rate.Provider)
		rate.Pattern = normalize(rate.Pattern)
		if !rate.usable() {
			out.Unusable++
			continue
		}
		rates = append(rates, rate)
	}

	// Sorted first so the winner of each key is simply the one seen first.
	sortBySpecificity(rates)

	seen := make(map[key]struct{}, len(rates))
	for _, rate := range rates {
		k := key{rate.Provider, rate.Pattern, rate.Operator}
		if _, dup := seen[k]; dup {
			out.Duplicates++
			continue
		}
		seen[k] = struct{}{}
		out.Rates = append(out.Rates, rate)
	}
	return out
}
