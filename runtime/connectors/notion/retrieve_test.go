package notion

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/types"
)

func TestRetrievePageFoldsResult(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"page","id":"p1","properties":{"Name":{"title":[]}}}`))
	}))
	defer srv.Close()

	proc, err := newRetrievePage(types.Settings{
		"connector": "notion",
		"page":      "body.entityId",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRetrievePage: %v", err)
	}

	msg := blockMessage(t, map[string]any{"entityId": "p1"})
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/pages/p1" {
		t.Errorf("path = %q, want /pages/p1", gotPath)
	}
	page, ok := out.Variables[defaultPageVar].(map[string]any)
	if !ok || page["id"] != "p1" {
		t.Errorf("%s = %v, want the page object", defaultPageVar, out.Variables[defaultPageVar])
	}
}

func TestRetrievePageRequiresPage(t *testing.T) {
	if _, err := newRetrievePage(types.Settings{"connector": "notion"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when page is not set")
	}
}

func TestRetrievePageToleratesErrorWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"object":"error","code":"object_not_found","message":"Not found"}`))
	}))
	defer srv.Close()

	failOnError := false
	proc, err := newRetrievePage(types.Settings{
		"connector":   "notion",
		"page":        `"missing"`,
		"failOnError": failOnError,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRetrievePage: %v", err)
	}

	msg := blockMessage(t, map[string]any{})
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process should tolerate the error, got: %v", err)
	}
	if out == nil {
		t.Fatal("expected the message to pass through when failOnError is false")
	}
	if _, ok := out.Variables[defaultPageVar]; ok {
		t.Error("did not expect a result variable on a tolerated error")
	}
}
