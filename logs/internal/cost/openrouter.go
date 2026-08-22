package cost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const (
	// SourceOpenRouter names this feed on every row it produces, beside the rows
	// helicone produces. The two are stored apart and diffed apart, so a model
	// priced by one is never quietly restated by the other.
	SourceOpenRouter = "openrouter"

	// DefaultOpenRouterCatalogueURL is OpenRouter's published model list, which
	// carries a price per model. It needs no API key: the list is what a caller
	// consults before deciding to buy anything.
	DefaultOpenRouterCatalogueURL = "https://openrouter.ai/api/v1/models"

	// variantSeparator splits a routing variant off a model id — :free, :thinking,
	// :online and friends. The variant is part of the id a call reports, so it is
	// kept on the exact pattern; it is dropped from the bare-slug patterns, which
	// exist to match ids from a different namespace entirely.
	variantSeparator = ":"

	// vendorSeparator splits an OpenRouter id into its vendor prefix and the
	// vendor's own name for the model: anthropic/claude-sonnet-4.5.
	vendorSeparator = "/"

	// minSlugLength bounds how short a bare slug may be before it is refused as a
	// prefix pattern. A two-character slug under startsWith would price a large
	// slice of the catalogue at one model's rate, which is the same failure
	// Rate.usable exists to prevent for an empty pattern — just further along the
	// same axis.
	minSlugLength = 4
)

// OpenRouterCatalogue reads OpenRouter's published model list over HTTP.
type OpenRouterCatalogue struct {
	url    string
	client *http.Client
}

// NewOpenRouterCatalogue returns a catalogue reader. An empty url falls back to
// DefaultOpenRouterCatalogueURL and a nil client to one carrying
// defaultFetchTimeout, so the zero configuration is the working one and a test
// can replace either.
func NewOpenRouterCatalogue(url string, client *http.Client) *OpenRouterCatalogue {
	if url == "" {
		url = DefaultOpenRouterCatalogueURL
	}
	if client == nil {
		client = &http.Client{Timeout: defaultFetchTimeout}
	}
	return &OpenRouterCatalogue{url: url, client: client}
}

// Fetch reads the model list and reduces it to rates that can be stored and
// applied.
func (c *OpenRouterCatalogue) Fetch(ctx context.Context) (Fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return Fetched{}, fmt.Errorf("cost: build openrouter request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("cost: fetch openrouter catalogue %q: %w", c.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Fetched{}, fmt.Errorf("cost: fetch openrouter catalogue %q: status %s", c.url, resp.Status)
	}

	var payload openRouterPayload
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxCatalogueBytes)).Decode(&payload); err != nil {
		return Fetched{}, fmt.Errorf("cost: decode openrouter catalogue %q: %w", c.url, err)
	}

	rows, skipped := openRouterRows(payload.Data)
	fetched := reduce(rows)
	// Counted rather than dropped quietly: a feed that suddenly prices nothing
	// should be visible as a number in the sync record, not as a card that got
	// smaller for no stated reason.
	fetched.Unusable += skipped
	if len(fetched.Rates) == 0 {
		return Fetched{}, errEmptyCatalogue
	}
	return fetched, nil
}

// openRouterPayload is the feed's envelope.
type openRouterPayload struct {
	Data []openRouterModel `json:"data"`
}

// openRouterModel is one published model.
//
// The feed carries much this does not name — context length, architecture,
// supported parameters, per-image and per-request charges. The last two are
// ignored on the same terms the helicone decoder ignores them: the runtime
// reports token counts and nothing else, so there is no quantity here to
// multiply them by. A model billed per call or per image is therefore
// under-costed by whatever that portion is — which is exactly the gap the
// provider-reported cost closes for calls that actually went through OpenRouter.
type openRouterModel struct {
	ID      string            `json:"id"`
	Pricing openRouterPricing `json:"pricing"`
}

// openRouterPricing is USD per token, published as strings so no reader has to
// trust a float round-trip of a number like 3e-7.
type openRouterPricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
}

// openRouterRows expands published models into catalogue rows.
//
// Each model yields up to three, because one entry has to price a call made
// THROUGH OpenRouter and a call made directly against the vendor, and those
// report model ids from different namespaces:
//
//   - equals on the full id, which is what a call routed through OpenRouter
//     reports (anthropic/claude-sonnet-4.5, variant suffix and all);
//   - startsWith on the bare slug, which is what the vendor's own API reports
//     (claude-sonnet-4.5);
//   - startsWith on the same slug with dots rewritten as dashes, because the
//     vendors do not agree with OpenRouter on that spelling — Anthropic's own id
//     for that model is claude-sonnet-4-5-20250929.
//
// The prefix patterns are safe against each other by construction: moreSpecific
// orders longest-first, so a catalogue holding both gpt-4 and gpt-4o prices
// gpt-4o-2024-11-20 from the longer one.
//
// The second return counts entries that could not become a rate at all, so a
// feed that stops pricing things says so rather than shrinking silently.
func openRouterRows(models []openRouterModel) (rows []catalogueRow, skipped int) {
	rows = make([]catalogueRow, 0, len(models))
	for _, m := range models {
		id := normalize(m.ID)
		provider := vendorOf(id)
		input, output := perMillion(m.Pricing.Prompt), perMillion(m.Pricing.Completion)

		// Three ways a published entry cannot become a rate, and the last is the
		// one that matters. A model the feed prices at nothing is a model it has
		// NOT priced -- the auto-router publishes "-1" for every field -- and
		// admitting it would put a zero rate in the card, which prices every call
		// it matches at free. That is the one wrong answer this package exists to
		// never give.
		if id == "" || provider == "" || (input == 0 && output == 0) {
			skipped++
			continue
		}

		cacheRead, cacheWrite := optionalPerMillion(m.Pricing.InputCacheRead),
			optionalPerMillion(m.Pricing.InputCacheWrite)

		add := func(pattern string, operator Operator) {
			rows = append(rows, catalogueRow{
				Provider: provider, Model: pattern, Operator: operator,
				InputPer1M: input, OutputPer1M: output,
				CacheReadPer1M: cacheRead, CacheWritePer1M: cacheWrite,
				// Every row here is the feed's current price for that model; the
				// feed publishes no second opinion to tell apart.
				Preferred: true,
			})
		}

		add(id, OpEquals)
		for _, slug := range bareSlugs(id) {
			add(slug, OpStartsWith)
		}
	}
	return rows, skipped
}

// vendorOf reads the vendor family off an OpenRouter id's prefix, in the
// vocabulary the rest of this package uses.
//
// It matters beyond identity: the provider on a rate is what prices a record
// whose own provider attribute is missing, which is every record written before
// the runtime stamped one. A row for anthropic/claude-sonnet-4.5 has to say
// ANTHROPIC so such a record is charged by Anthropic's exclusive cached-token
// rule rather than the inclusive one.
//
// A call that actually went through OpenRouter reports OPENROUTER on the record
// itself, and that wins — correctly, because those counts follow the inclusive
// convention whatever vendor served them.
func vendorOf(id string) string {
	vendor, _, found := strings.Cut(id, vendorSeparator)
	if !found {
		return ""
	}
	return normalizeProvider(vendor)
}

// bareSlugs renders the vendor's own likely spellings of a model id, deduped and
// with anything too short to be a safe prefix dropped.
func bareSlugs(id string) []string {
	_, slug, found := strings.Cut(id, vendorSeparator)
	if !found {
		return nil
	}
	// The variant is OpenRouter's own routing vocabulary, so it belongs to the
	// full id and never to a vendor's spelling of the model.
	slug, _, _ = strings.Cut(slug, variantSeparator)
	if len(slug) < minSlugLength {
		return nil
	}

	out := []string{slug}
	if dashed := strings.ReplaceAll(slug, ".", "-"); dashed != slug {
		out = append(out, dashed)
	}
	return out
}

// perMillion converts a published per-token price to the per-million figure every
// rate in this package is quoted in. Anything that is not a price reads as zero,
// which prices nothing rather than pricing wrongly.
//
// The check is on the CONVERTED figure rather than the parsed one, and that is
// not tidiness: it subsumes the parsed check, and it also catches the one case
// the parsed check cannot see — a value large but finite on its own that
// overflows to infinity once scaled by a million. The feed's "-1" for variable
// pricing needs no case of its own; it is simply not a non-negative number.
func perMillion(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	per1M := value * tokensPerUnit
	if !usablePrice(per1M) {
		return 0
	}
	return per1M
}

// optionalPerMillion is perMillion for the two cache halves, which are absent
// rather than zero when a model publishes no cache price. Nil is a different
// fact from a rate of zero and decides whether cached tokens can be priced at
// all, so an absent field stays absent all the way to the rate.
//
// A published figure that is not a price is treated as an absent one, which is
// what keeps a model with one bad cache half priceable: it falls back to the
// input rate and reports priced_partial, the same answer a model that published
// no cache rate at all gets.
func optionalPerMillion(raw string) *float64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return nil
	}
	per1M := value * tokensPerUnit
	if !usablePrice(per1M) {
		return nil
	}
	return &per1M
}
