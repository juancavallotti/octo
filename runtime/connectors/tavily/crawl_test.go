package tavily

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

const crawlResponse = `{"base_url":"https://example.com","results":[{"url":"https://example.com/a"}]}`

// traversalSettings is the traversal half both tavily-crawl and tavily-map
// accept, used to prove the two build the same request fields from it.
func traversalSettings() types.Settings {
	return types.Settings{
		"connector":      "tavily",
		"url":            "body.site",
		"instructions":   `"find the pricing pages"`,
		"maxDepth":       3,
		"maxBreadth":     50,
		"limit":          200,
		"selectPaths":    `["/pricing/.*"]`,
		"excludePaths":   `["/blog/.*"]`,
		"selectDomains":  `["^example\\.com$"]`,
		"excludeDomains": "body.blocked",
		"allowExternal":  false,
	}
}

// assertTraversalPayload checks the request fields crawl and map share.
func assertTraversalPayload(t *testing.T, got map[string]any) {
	t.Helper()
	want := map[string]any{
		"url":            "https://example.com",
		"instructions":   "find the pricing pages",
		"max_depth":      float64(3),
		"max_breadth":    float64(50),
		"limit":          float64(200),
		"allow_external": false,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("request %s = %v, want %v", key, got[key], value)
		}
	}
	for _, key := range []string{"select_paths", "exclude_paths", "select_domains", "exclude_domains"} {
		if items, _ := got[key].([]any); len(items) != 1 {
			t.Errorf("%s = %v, want a one-element list", key, got[key])
		}
	}
}

// traversalMessage carries the fields traversalSettings reads.
func traversalMessage(t *testing.T) *types.Message {
	t.Helper()
	return blockMessage(t, map[string]any{
		"site":    "https://example.com",
		"blocked": []any{"spam.example"},
	})
}

func TestCrawlSendsTraversalAndExtractionOptions(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got, crawlResponse)

	cfg := traversalSettings()
	cfg["extractDepth"] = "advanced"
	cfg["format"] = "text"

	proc, err := newCrawl(cfg, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCrawl: %v", err)
	}
	out, err := proc.Process(context.Background(), traversalMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	assertTraversalPayload(t, got)
	if got["extract_depth"] != "advanced" {
		t.Errorf("extract_depth = %v, want advanced", got["extract_depth"])
	}
	if got["format"] != "text" {
		t.Errorf("format = %v, want text", got["format"])
	}
	if body, _ := out.Body.(map[string]any); body["base_url"] != "https://example.com" {
		t.Errorf("body = %v, want the crawl response", out.Body)
	}
}

func TestCrawlOmitsUnsetTraversalOptions(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got, crawlResponse)

	proc, err := newCrawl(types.Settings{
		"connector": "tavily",
		"url":       `"https://example.com"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCrawl: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	for _, key := range []string{
		"instructions", "max_depth", "max_breadth", "limit",
		"select_paths", "exclude_paths", "allow_external", "extract_depth", "format",
	} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when unset, got %v", key, got[key])
		}
	}
}

func TestCrawlSendsAllowExternalWhenExplicit(t *testing.T) {
	for _, allow := range []bool{true, false} {
		var got map[string]any
		srv := jsonStub(t, &got, crawlResponse)

		proc, err := newCrawl(types.Settings{
			"connector":     "tavily",
			"url":           `"https://example.com"`,
			"allowExternal": allow,
		}, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newCrawl: %v", err)
		}
		if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if got["allow_external"] != allow {
			t.Errorf("allow_external = %v, want %v", got["allow_external"], allow)
		}
	}
}

func TestCrawlRequiresURL(t *testing.T) {
	if _, err := newCrawl(types.Settings{"connector": "tavily"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when url is not set")
	}
}

func TestCrawlRejectsBadExpressions(t *testing.T) {
	cases := []types.Settings{
		{"connector": "tavily", "url": "body."},
		{"connector": "tavily", "url": `"u"`, "instructions": "body."},
		{"connector": "tavily", "url": `"u"`, "selectPaths": "body."},
		{"connector": "tavily", "url": `"u"`, "excludeDomains": "body."},
	}
	for _, cfg := range cases {
		if _, err := newCrawl(cfg, blockDeps(t, "")); err == nil {
			t.Errorf("expected a compile error for %v", cfg)
		}
	}
}

func TestCrawlToleratesErrorWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":"bad url"}`))
	}))
	defer srv.Close()

	failOnError := false
	proc, err := newCrawl(types.Settings{
		"connector":   "tavily",
		"url":         `"https://example.com"`,
		"failOnError": &failOnError,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCrawl: %v", err)
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
