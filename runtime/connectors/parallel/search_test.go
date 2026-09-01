package parallel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

const searchResponse = `{"search_id":"s1","results":[` +
	`{"url":"https://a.test","title":"A","excerpts":["one"]}],"session_id":"x"}`

// jsonStub answers every request with searchResponse and records the request it
// was given.
func jsonStub(t *testing.T, got *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(searchResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSearchSendsObjectiveAndQueries(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":     "parallel",
		"objective":     "body.goal",
		"searchQueries": "body.queries",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{
		"goal":    "find octopus anatomy papers",
		"queries": []any{"octopus arm anatomy", "cephalopod musculature"},
	}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got["objective"] != "find octopus anatomy papers" {
		t.Errorf("objective = %v", got["objective"])
	}
	queries, _ := got["search_queries"].([]any)
	if len(queries) != 2 || queries[0] != "octopus arm anatomy" {
		t.Errorf("search_queries = %v, want both queries", got["search_queries"])
	}
	if body, _ := out.Body.(map[string]any); body["search_id"] != "s1" {
		t.Errorf("body = %v, want the search response", out.Body)
	}
}

func TestSearchAcceptsASingleQueryString(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":     "parallel",
		"objective":     `"anything"`,
		"searchQueries": "body.q",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, map[string]any{
		"q": "one query",
	})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if queries, _ := got["search_queries"].([]any); len(queries) != 1 || queries[0] != "one query" {
		t.Errorf("search_queries = %v, want a one-element list", got["search_queries"])
	}
}

func TestSearchSendsConfiguredOptions(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":         "parallel",
		"objective":         `"anything"`,
		"searchQueries":     `["q"]`,
		"mode":              "turbo",
		"processor":         "base",
		"maxResults":        7,
		"maxCharsPerResult": 1500,
		"resultVar":         "hits",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"in": true}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := map[string]any{
		"mode":                 "turbo",
		"processor":            "base",
		"max_results":          float64(7),
		"max_chars_per_result": float64(1500),
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("request %s = %v, want %v", key, got[key], value)
		}
	}
	if out.Variables["hits"] == nil {
		t.Error("hits should hold the response when resultVar is set")
	}
	if body, _ := out.Body.(map[string]any); body["in"] != true {
		t.Error("resultVar should leave the body alone")
	}
}

func TestSearchOmitsUnsetOptions(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":     "parallel",
		"objective":     `"anything"`,
		"searchQueries": `["q"]`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	for _, key := range []string{"mode", "processor", "max_results", "max_chars_per_result"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when unset, got %v", key, got[key])
		}
	}
}

func TestSearchRequiresObjectiveAndQueries(t *testing.T) {
	cases := []types.Settings{
		{"connector": "parallel", "searchQueries": `["q"]`},
		{"connector": "parallel", "objective": `"o"`},
		{"connector": "parallel", "objective": "body.", "searchQueries": `["q"]`},
		{"connector": "parallel", "objective": `"o"`, "searchQueries": "body."},
	}
	for _, cfg := range cases {
		if _, err := newSearch(cfg, blockDeps(t, "")); err == nil {
			t.Errorf("expected an error for %v", cfg)
		}
	}
}

func TestSearchRejectsNonListQueries(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got)

	proc, err := newSearch(types.Settings{
		"connector":     "parallel",
		"objective":     `"o"`,
		"searchQueries": "42",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected an error when searchQueries does not evaluate to strings")
	}
}

func TestSearchToleratesErrorWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
	}))
	defer srv.Close()

	failOnError := false
	proc, err := newSearch(types.Settings{
		"connector":     "parallel",
		"objective":     `"o"`,
		"searchQueries": `["q"]`,
		"failOnError":   &failOnError,
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
		_, _ = w.Write([]byte(`{"detail":"Invalid API key"}`))
	}))
	defer srv.Close()

	proc, err := newSearch(types.Settings{
		"connector":     "parallel",
		"objective":     `"o"`,
		"searchQueries": `["q"]`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearch: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected the API error to abort the flow")
	}
}
