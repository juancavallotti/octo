package tavily

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

const mapResponse = `{"base_url":"https://example.com","results":["https://example.com/a"]}`

func TestMapSendsTraversalOptions(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got, mapResponse)

	proc, err := newMap(traversalSettings(), blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newMap: %v", err)
	}
	out, err := proc.Process(context.Background(), traversalMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	assertTraversalPayload(t, got)
	// map returns URLs only, so it must not send the extraction knobs crawl does.
	for _, key := range []string{"extract_depth", "format"} {
		if _, ok := got[key]; ok {
			t.Errorf("map should not send %s, got %v", key, got[key])
		}
	}
	body, _ := out.Body.(map[string]any)
	if results, _ := body["results"].([]any); len(results) != 1 {
		t.Errorf("body results = %v, want the discovered URLs", body["results"])
	}
}

func TestMapPostsToMapPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(mapResponse))
	}))
	defer srv.Close()

	proc, err := newMap(types.Settings{
		"connector": "tavily",
		"url":       `"https://example.com"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newMap: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if gotPath != "/map" {
		t.Errorf("path = %q, want /map", gotPath)
	}
}

func TestMapRequiresURL(t *testing.T) {
	if _, err := newMap(types.Settings{"connector": "tavily"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when url is not set")
	}
}

func TestMapRejectsBadURLExpression(t *testing.T) {
	if _, err := newMap(types.Settings{"connector": "tavily", "url": "body."}, blockDeps(t, "")); err == nil {
		t.Error("expected a compile error for a malformed url expression")
	}
}

func TestMapReportsEvalErrors(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got, mapResponse)

	proc, err := newMap(types.Settings{
		"connector":   "tavily",
		"url":         `"https://example.com"`,
		"selectPaths": "42",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newMap: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected an error when selectPaths does not evaluate to a list")
	}
}
