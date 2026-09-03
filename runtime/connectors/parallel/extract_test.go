package parallel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

const extractResponse = `{"extract_id":"e1","results":[` +
	`{"url":"https://a.test","title":"A","excerpts":["one"]}],"session_id":"x"}`

// extractStub answers every request with extractResponse and records the request
// it was given.
func extractStub(t *testing.T, got *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(extractResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExtractSendsURLsAndObjective(t *testing.T) {
	var got map[string]any
	srv := extractStub(t, &got)

	proc, err := newExtract(types.Settings{
		"connector": "parallel",
		"urls":      "body.urls",
		"objective": "body.goal",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{
		"urls": []any{"https://a.test", "https://b.test"},
		"goal": "when was it founded",
	}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	urls, _ := got["urls"].([]any)
	if len(urls) != 2 || urls[0] != "https://a.test" {
		t.Errorf("urls = %v, want both urls", got["urls"])
	}
	if got["objective"] != "when was it founded" {
		t.Errorf("objective = %v", got["objective"])
	}
	if body, _ := out.Body.(map[string]any); body["extract_id"] != "e1" {
		t.Errorf("body = %v, want the extract response", out.Body)
	}
}

func TestExtractAcceptsASingleURLString(t *testing.T) {
	var got map[string]any
	srv := extractStub(t, &got)

	proc, err := newExtract(types.Settings{
		"connector": "parallel",
		"urls":      "body.url",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, map[string]any{
		"url": "https://a.test",
	})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if urls, _ := got["urls"].([]any); len(urls) != 1 || urls[0] != "https://a.test" {
		t.Errorf("urls = %v, want a one-element list", got["urls"])
	}
}

func TestExtractSendsFullContentAndResultVar(t *testing.T) {
	var got map[string]any
	srv := extractStub(t, &got)

	proc, err := newExtract(types.Settings{
		"connector":   "parallel",
		"urls":        `["https://a.test"]`,
		"fullContent": true,
		"resultVar":   "pages",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"in": true}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got["full_content"] != true {
		t.Errorf("full_content = %v, want true", got["full_content"])
	}
	if out.Variables["pages"] == nil {
		t.Error("pages should hold the response when resultVar is set")
	}
	if body, _ := out.Body.(map[string]any); body["in"] != true {
		t.Error("resultVar should leave the body alone")
	}
}

func TestExtractOmitsUnsetOptions(t *testing.T) {
	var got map[string]any
	srv := extractStub(t, &got)

	proc, err := newExtract(types.Settings{
		"connector": "parallel",
		"urls":      `["https://a.test"]`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	for _, key := range []string{"objective", "full_content"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when unset, got %v", key, got[key])
		}
	}
}

func TestExtractRequiresURLs(t *testing.T) {
	cases := []types.Settings{
		{"connector": "parallel"},
		{"connector": "parallel", "urls": "body."},
		{"connector": "parallel", "urls": `["u"]`, "objective": "body."},
	}
	for _, cfg := range cases {
		if _, err := newExtract(cfg, blockDeps(t, "")); err == nil {
			t.Errorf("expected an error for %v", cfg)
		}
	}
}

func TestExtractRejectsNonListURLs(t *testing.T) {
	var got map[string]any
	srv := extractStub(t, &got)

	proc, err := newExtract(types.Settings{
		"connector": "parallel",
		"urls":      "42",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected an error when urls does not evaluate to strings")
	}
}

func TestExtractToleratesErrorWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"detail":"rate limited"}`))
	}))
	defer srv.Close()

	failOnError := false
	proc, err := newExtract(types.Settings{
		"connector":   "parallel",
		"urls":        `["https://a.test"]`,
		"failOnError": &failOnError,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
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

func TestExtractFailsOnErrorByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"Invalid API key"}`))
	}))
	defer srv.Close()

	proc, err := newExtract(types.Settings{
		"connector": "parallel",
		"urls":      `["https://a.test"]`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected the API error to abort the flow")
	}
}
