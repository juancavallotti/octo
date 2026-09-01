package tavily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// searchStub answers every request with a fixed search response and records the
// request body it was given.
func searchStub(t *testing.T, got *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"octo","results":[{"url":"https://example.com","score":0.9}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchDeliversResponseAsBody(t *testing.T) {
	var got map[string]any
	srv := searchStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector": "tavily",
		"query":     "body.q",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"q": "octo"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got["query"] != "octo" {
		t.Errorf("request query = %v, want octo", got["query"])
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["query"] != "octo" {
		t.Errorf("body = %v, want the search response", out.Body)
	}
}

func TestSearchSendsConfiguredOptions(t *testing.T) {
	var got map[string]any
	srv := searchStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":         "tavily",
		"query":             `"octo"`,
		"searchDepth":       "advanced",
		"topic":             "news",
		"maxResults":        10,
		"chunksPerSource":   2,
		"includeAnswer":     "advanced",
		"includeRawContent": "markdown",
		"includeDomains":    `["example.com"]`,
		"excludeDomains":    "body.blocked",
		"country":           "uruguay",
		"language":          "es",
		"resultVar":         "hits",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{
		"blocked": []any{"spam.example"},
	}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := map[string]any{
		"query":               "octo",
		"search_depth":        "advanced",
		"topic":               "news",
		"max_results":         float64(10),
		"chunks_per_source":   float64(2),
		"include_answer":      "advanced",
		"include_raw_content": "markdown",
		"country":             "uruguay",
		"language":            "es",
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("request %s = %v, want %v", key, got[key], value)
		}
	}
	if domains, _ := got["include_domains"].([]any); len(domains) != 1 || domains[0] != "example.com" {
		t.Errorf("include_domains = %v, want [example.com]", got["include_domains"])
	}
	if domains, _ := got["exclude_domains"].([]any); len(domains) != 1 || domains[0] != "spam.example" {
		t.Errorf("exclude_domains = %v, want [spam.example]", got["exclude_domains"])
	}
	if out.Variables["hits"] == nil {
		t.Error("hits should hold the response when resultVar is set")
	}
}

func TestSearchOmitsUnsetOptions(t *testing.T) {
	var got map[string]any
	srv := searchStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":         "tavily",
		"query":             `"octo"`,
		"includeAnswer":     "none",
		"includeRawContent": "none",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}

	for _, key := range []string{
		"include_answer", "include_raw_content", "max_results",
		"search_depth", "include_domains", "exclude_domains",
	} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when unset, got %v", key, got[key])
		}
	}
}

func TestSearchRequiresQuery(t *testing.T) {
	if _, err := newSearch(types.Settings{"connector": "tavily"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when query is not set")
	}
}

func TestSearchRejectsBadExpressions(t *testing.T) {
	cases := []types.Settings{
		{"connector": "tavily", "query": "body."},
		{"connector": "tavily", "query": `"q"`, "includeDomains": "body."},
		{"connector": "tavily", "query": `"q"`, "excludeDomains": "body."},
	}
	for _, cfg := range cases {
		if _, err := newSearch(cfg, blockDeps(t, "")); err == nil {
			t.Errorf("expected a compile error for %v", cfg)
		}
	}
}

func TestSearchRejectsNonListDomains(t *testing.T) {
	var got map[string]any
	srv := searchStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":      "tavily",
		"query":          `"octo"`,
		"includeDomains": `"example.com"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	// A bare string is accepted as a one-element list, matching Tavily's own
	// tolerance; anything else is a flow error.
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if domains, _ := got["include_domains"].([]any); len(domains) != 1 {
		t.Errorf("include_domains = %v, want a one-element list", got["include_domains"])
	}

	proc, err = newSearch(types.Settings{
		"connector":      "tavily",
		"query":          `"octo"`,
		"includeDomains": "42",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected an error for a non-list includeDomains")
	}
}

func TestSearchToleratesErrorWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":{"error":"rate limited"}}`))
	}))
	defer srv.Close()

	failOnError := false
	proc, err := newSearch(types.Settings{
		"connector":   "tavily",
		"query":       `"octo"`,
		"failOnError": &failOnError,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}

	msg := blockMessage(t, map[string]any{"in": true})
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process should tolerate the error: %v", err)
	}
	if body, _ := out.Body.(map[string]any); body["in"] != true {
		t.Error("a tolerated error should leave the message unchanged")
	}
}

func TestSearchFailsOnErrorByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":{"error":"Invalid API key"}}`))
	}))
	defer srv.Close()

	proc, err := newSearch(types.Settings{
		"connector": "tavily",
		"query":     `"octo"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected the API error to abort the flow")
	}
}
