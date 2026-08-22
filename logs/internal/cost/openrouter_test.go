package cost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// serveOpenRouterFixture stands up the published model list from testdata, whose
// shapes are the ones the real feed produces: per-token prices as strings, cache
// halves present on some models and absent on others, a routing variant, an
// auto-router that prices nothing, and ids the decoder has to refuse.
func serveOpenRouterFixture(t *testing.T) *OpenRouterCatalogue {
	t.Helper()
	body, err := os.ReadFile("testdata/openrouter-models.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return NewOpenRouterCatalogue(server.URL, server.Client())
}

func fetchOpenRouter(t *testing.T) Fetched {
	t.Helper()
	got, err := serveOpenRouterFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	return got
}

// rateFor finds the entry a table would resolve for a served model id.
func rateFor(t *testing.T, fetched Fetched, model string) Rate {
	t.Helper()
	rate, found := NewTable(fetched.Rates).Rate(model)
	if !found {
		t.Fatalf("no rate matched %q", model)
	}
	return rate
}

func TestOpenRouterFetch(t *testing.T) {
	got := fetchOpenRouter(t)

	// Ten published models: three cannot become a rate at all (no id, no vendor
	// prefix, priced at nothing), and the thinking variant's two bare-slug rows
	// duplicate the base model's.
	if got.Unusable != 3 {
		t.Errorf("unusable = %d, want 3", got.Unusable)
	}
	if got.Duplicates != 2 {
		t.Errorf("duplicates = %d, want 2", got.Duplicates)
	}
	if len(got.Rates) != 14 {
		t.Errorf("rates = %d, want 14", len(got.Rates))
	}
}

// Prices arrive per token as strings and every rate in this package is quoted
// per million, so this is the conversion the whole card depends on.
func TestOpenRouterConvertsPerTokenToPerMillion(t *testing.T) {
	rate := rateFor(t, fetchOpenRouter(t), "anthropic/claude-sonnet-4.5")

	if rate.InputPer1M != 3 || rate.OutputPer1M != 15 {
		t.Errorf("rates = %v/%v per 1M, want 3/15", rate.InputPer1M, rate.OutputPer1M)
	}
	if rate.CacheReadPer1M == nil || *rate.CacheReadPer1M != 0.3 {
		t.Errorf("cache read = %v, want 0.3", rate.CacheReadPer1M)
	}
	if rate.CacheWritePer1M == nil || *rate.CacheWritePer1M != 3.75 {
		t.Errorf("cache write = %v, want 3.75", rate.CacheWritePer1M)
	}
}

// Absent is not zero. A model publishing no cache rate must leave the half nil,
// because nil is what makes Table.Price fall back and say so with
// priced_partial rather than charging cached tokens at nothing.
func TestOpenRouterLeavesAbsentCacheRatesAbsent(t *testing.T) {
	rate := rateFor(t, fetchOpenRouter(t), "google/gemini-2.5-flash")
	if rate.CacheReadPer1M != nil || rate.CacheWritePer1M != nil {
		t.Errorf("cache rates = %v/%v, want both absent", rate.CacheReadPer1M, rate.CacheWritePer1M)
	}
}

// The vendor prefix is the provider, and it is not decoration: it decides how a
// record with no provider of its own has its cached tokens charged.
func TestOpenRouterReadsTheProviderFromTheVendorPrefix(t *testing.T) {
	fetched := fetchOpenRouter(t)
	for model, want := range map[string]string{
		"anthropic/claude-sonnet-4.5": "ANTHROPIC",
		"openai/gpt-4o":               "OPENAI",
		"google/gemini-2.5-flash":     "GOOGLE",
		"x-ai/grok-4":                 "X-AI",
	} {
		if got := rateFor(t, fetched, model).Provider; got != want {
			t.Errorf("provider for %q = %q, want %q", model, got, want)
		}
	}
}

// The reason for the bare-slug patterns: one catalogue entry has to price a call
// made through OpenRouter and the same model called directly, and those report
// ids from different namespaces.
func TestOpenRouterPricesDirectVendorModelIds(t *testing.T) {
	fetched := fetchOpenRouter(t)

	tests := []struct {
		name  string
		model string
		want  float64
	}{
		{name: "routed through openrouter", model: "anthropic/claude-sonnet-4.5", want: 3},
		{name: "openrouter's own spelling", model: "claude-sonnet-4.5", want: 3},
		// Anthropic's own id for that model, which spells the version with
		// dashes and carries a date.
		{name: "anthropic's dated snapshot", model: "claude-sonnet-4-5-20250929", want: 3},
		{name: "openai's own id", model: "gpt-4o-2024-11-20", want: 2.5},
		{name: "gemini preview snapshot", model: "gemini-2.5-flash-preview-09-2025", want: 0.3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rateFor(t, fetched, tc.model).InputPer1M; got != tc.want {
				t.Errorf("input rate for %q = %v, want %v", tc.model, got, tc.want)
			}
		})
	}
}

// A shorter slug must not swallow a longer model's id. moreSpecific orders
// longest-pattern-first, which is what makes emitting both gpt-4 and gpt-4o
// safe rather than a trap.
func TestOpenRouterPrefersTheLongerSlug(t *testing.T) {
	rate := rateFor(t, fetchOpenRouter(t), "gpt-4o-2024-11-20")
	if rate.Pattern != "gpt-4o" {
		t.Errorf("pattern = %q, want the longer gpt-4o rather than gpt-4", rate.Pattern)
	}
}

// A routing variant belongs to OpenRouter's own id and never to a vendor's
// spelling, so it is kept on the exact pattern and stripped from the slugs.
func TestOpenRouterKeepsTheVariantOnlyOnTheExactPattern(t *testing.T) {
	fetched := fetchOpenRouter(t)

	if got := rateFor(t, fetched, "anthropic/claude-sonnet-4.5:thinking").Operator; got != OpEquals {
		t.Errorf("operator = %q, want the variant matched exactly", got)
	}
	for _, rate := range fetched.Rates {
		if rate.Operator == OpStartsWith && rate.Pattern == "claude-sonnet-4.5:thinking" {
			t.Error("a variant suffix leaked into a bare-slug pattern")
		}
	}
}

// The one that would be silent: the auto-router publishes "-1" for every price,
// meaning variable, and its bare slug is short enough to prefix-match plenty. A
// zero rate admitted here would report every matching call as free.
func TestOpenRouterRefusesAModelItPricesAtNothing(t *testing.T) {
	fetched := fetchOpenRouter(t)
	for _, rate := range fetched.Rates {
		if rate.Pattern == "openrouter/auto" || rate.Pattern == "auto" {
			t.Fatalf("an unpriced model was admitted to the card: %+v", rate)
		}
		if rate.InputPer1M == 0 && rate.OutputPer1M == 0 {
			t.Errorf("a rate that prices nothing was admitted: %+v", rate)
		}
	}
	if _, found := NewTable(fetched.Rates).Rate("autopilot-9000"); found {
		t.Error("a short unpriced slug matched an unrelated model")
	}
}

// A slug too short to be a safe prefix is dropped rather than emitted; the exact
// id still prices the model.
func TestOpenRouterDropsASlugTooShortToPrefixSafely(t *testing.T) {
	fetched := fetchOpenRouter(t)
	for _, rate := range fetched.Rates {
		if rate.Operator == OpStartsWith && len(rate.Pattern) < minSlugLength {
			t.Errorf("a slug shorter than the floor was emitted: %+v", rate)
		}
	}
	if _, found := NewTable(fetched.Rates).Rate("meta-llama/l3"); !found {
		t.Error("the exact id should still price the model")
	}
}

func TestOpenRouterFetchErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{
			name: "not ok",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
		},
		{
			name: "not json",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("{["))
			},
		},
		{
			// A 200 carrying no usable rows is far more likely an upstream fault
			// than a real state of the world, and believing it would report every
			// model call as unpriced.
			name: "prices nothing",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data":[]}`))
			},
			wantErr: errEmptyCatalogue,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			_, err := NewOpenRouterCatalogue(server.URL, server.Client()).Fetch(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewOpenRouterCatalogueDefaults(t *testing.T) {
	c := NewOpenRouterCatalogue("", nil)
	if c.url != DefaultOpenRouterCatalogueURL {
		t.Errorf("url = %q, want the published default", c.url)
	}
	if c.client == nil || c.client.Timeout != defaultFetchTimeout {
		t.Errorf("client = %+v, want one carrying the default timeout", c.client)
	}
}

// The two sources are named apart, so a stored price says which feed produced it
// and neither can quietly restate the other's history.
func TestOpenRouterSourceIsDistinct(t *testing.T) {
	if SourceOpenRouter == SourceHelicone {
		t.Error("the two sources must be distinguishable on a stored row")
	}
}
