package cost

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// serveFixture stands up the published card from testdata, which is a verbatim
// slice of the real feed: the duplicate keys, the optional cache rates, the
// current-price flag and the extra billing keys are all as published, so the
// decoder is tested against shapes the feed actually produces rather than
// shapes convenient to assert.
func serveFixture(t *testing.T) *Catalogue {
	t.Helper()
	body, err := os.ReadFile("testdata/llm-costs.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return NewCatalogue(server.URL, server.Client())
}

func TestFetch(t *testing.T) {
	got, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	// Twelve published rows: two cannot be applied, two are duplicate keys.
	if got.Unusable != 2 {
		t.Errorf("unusable = %d, want 2", got.Unusable)
	}
	if got.Duplicates != 2 {
		t.Errorf("duplicates = %d, want 2", got.Duplicates)
	}
	if len(got.Rates) != 8 {
		t.Errorf("rates = %d, want 8", len(got.Rates))
	}
}

func TestFetchDecodesAPublishedRow(t *testing.T) {
	got, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	rate, ok := NewTable(got.Rates).Rate("claude-3-5-sonnet-20241022")
	if !ok {
		t.Fatal("the fetched card does not price claude-3-5-sonnet-20241022")
	}
	switch {
	case rate.Provider != "ANTHROPIC":
		t.Errorf("provider = %q, want ANTHROPIC", rate.Provider)
	case rate.Operator != OpEquals:
		t.Errorf("operator = %q, want equals", rate.Operator)
	case rate.InputPer1M != 3 || rate.OutputPer1M != 15:
		t.Errorf("rates = %v/%v, want 3/15", rate.InputPer1M, rate.OutputPer1M)
	case rate.CacheReadPer1M == nil || *rate.CacheReadPer1M != 0.3:
		t.Errorf("cache read = %v, want 0.3", rate.CacheReadPer1M)
	case rate.CacheWritePer1M == nil || *rate.CacheWritePer1M != 3.75:
		t.Errorf("cache write = %v, want 3.75", rate.CacheWritePer1M)
	case !rate.Preferred:
		t.Error("show_in_playground did not survive the decode")
	}
}

// A published row with no cache keys must leave them absent, not zero: pricing
// reads nil as "this model publishes no cache rate" and charges cached tokens at
// the input rate, while zero would charge them at nothing.
func TestFetchLeavesAbsentCacheRatesAbsent(t *testing.T) {
	got, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	rate, ok := NewTable(got.Rates).Rate("gemini-2.5-flash-lite-preview-06-17")
	if !ok {
		t.Fatal("the fetched card does not price the gemini flash-lite family")
	}
	if rate.CacheReadPer1M != nil || rate.CacheWritePer1M != nil {
		t.Fatalf("absent cache rates decoded as %v/%v, want nil", rate.CacheReadPer1M, rate.CacheWritePer1M)
	}
}

// The feed publishes billing dimensions the runtime reports no quantity for
// (per_call, per_image, audio tokens). They must not break the decode; the rows
// carrying them still price on tokens.
func TestFetchIgnoresBillingKeysWeHaveNoQuantityFor(t *testing.T) {
	got, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	rate, ok := NewTable(got.Rates).Rate("sonar")
	if !ok {
		t.Fatal("a row carrying per_call was dropped, want it priced on tokens")
	}
	if rate.InputPer1M != 1 || rate.OutputPer1M != 1 {
		t.Fatalf("rates = %v/%v, want 1/1", rate.InputPer1M, rate.OutputPer1M)
	}
}

func TestFetchReducesDuplicateKeys(t *testing.T) {
	got, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	card := NewTable(got.Rates)

	cases := []struct {
		name      string
		model     string
		wantInput float64
		why       string
	}{
		{
			name:      "the feed's own current-price flag decides",
			model:     "gpt-4o-mini",
			wantInput: 0.15,
			why:       "AZURE publishes 0.20 and 0.15; only 0.15 carries show_in_playground",
		},
		{
			name:      "with no flag to go on, the cheaper price wins",
			model:     "meta-llama/meta-llama-3.1-405b-instruct-turbo",
			wantInput: 3.5,
			why:       "TOGETHER publishes 3.5 and 5.0 with nothing to tell them apart",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rate, ok := card.Rate(tc.model)
			if !ok {
				t.Fatalf("%q is not priced", tc.model)
			}
			if rate.InputPer1M != tc.wantInput {
				t.Fatalf("input rate = %v, want %v (%s)", rate.InputPer1M, tc.wantInput, tc.why)
			}
		})
	}
}

// Every key must survive reduction exactly once, or the stored card's unique
// index rejects the whole write rather than the offending row.
func TestFetchLeavesNoDuplicateKeys(t *testing.T) {
	got, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	seen := make(map[[3]string]int)
	for _, rate := range got.Rates {
		seen[[3]string{rate.Provider, rate.Pattern, string(rate.Operator)}]++
	}
	for key, n := range seen {
		if n > 1 {
			t.Errorf("%v survived reduction %d times, want 1", key, n)
		}
	}
}

// A row this package cannot apply must be dropped rather than stored. The empty
// pattern is the dangerous one: under includes it prices every model there is.
func TestFetchDropsRowsItCannotApply(t *testing.T) {
	got, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	for _, rate := range got.Rates {
		if rate.Provider == "MYSTERY" {
			t.Fatalf("kept an unusable row: %+v", rate)
		}
	}
	if _, ok := NewTable(got.Rates).Rate("anything-at-all"); ok {
		t.Fatal("an unusable row is pricing models it should not")
	}
}

func TestFetchErrors(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "a non-200 is not a card",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
		},
		{
			name: "a body that is not the expected JSON is not a card",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data": "this should be an array"}`))
			},
		},
		{
			name: "truncated JSON is not a card",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data": [{"provider": "OPENAI",`))
			},
		},
		{
			// A 200 pricing nothing is far more likely to be an upstream fault
			// than a real state of the world, and accepting it would report
			// every model call as unpriced.
			name: "a card that prices nothing is refused",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"data": []}`))
			},
		},
		{
			name: "a card whose every row is unusable is refused",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(
					`{"data": [{"provider":"X","model":"","operator":"includes",` +
						`"input_cost_per_1m":1,"output_cost_per_1m":1}]}`))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			if _, err := NewCatalogue(server.URL, server.Client()).Fetch(context.Background()); err == nil {
				t.Fatal("fetch succeeded, want an error")
			}
		})
	}
}

// A refresh must abandon its fetch when the service is shutting down rather than
// hold termination open for the request timeout.
func TestFetchHonoursContextCancellation(t *testing.T) {
	blocked := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-blocked
	}))
	defer server.Close()
	defer close(blocked)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewCatalogue(server.URL, server.Client()).Fetch(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to wrap context.Canceled", err)
	}
}

// The zero configuration is the working one, so a caller that supplies neither a
// url nor a client still points at the published card.
func TestNewCatalogueDefaults(t *testing.T) {
	c := NewCatalogue("", nil)
	if c.url != DefaultCatalogueURL {
		t.Errorf("url = %q, want %q", c.url, DefaultCatalogueURL)
	}
	if c.client == nil || c.client.Timeout != defaultFetchTimeout {
		t.Errorf("client = %+v, want one carrying %v", c.client, defaultFetchTimeout)
	}
}
