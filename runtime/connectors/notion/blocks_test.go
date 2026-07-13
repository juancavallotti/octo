package notion

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestRetrieveBlocksPaginatesAndSetsBody(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.String())
		w.Header().Set("Content-Type", "application/json")
		// First page has_more with a cursor; second page ends the listing.
		if r.URL.Query().Get("start_cursor") == "" {
			_, _ = w.Write([]byte(`{"object":"list","results":[{"id":"b1"}],"has_more":true,"next_cursor":"cur2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","results":[{"id":"b2"}],"has_more":false,"next_cursor":null}`))
	}))
	defer srv.Close()

	proc, err := newRetrieveBlocks(types.Settings{
		"connector": "notion",
		"block":     "body.pageId",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRetrieveBlocks: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"pageId": "p1"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("expected 2 paginated requests, got %v", paths)
	}
	want := fmt.Sprintf("/blocks/p1/children?page_size=%d", blocksPageSize)
	if paths[0] != want {
		t.Errorf("first path = %q, want %q", paths[0], want)
	}
	if paths[1] != want+"&start_cursor=cur2" {
		t.Errorf("second path = %q, want it to carry start_cursor=cur2", paths[1])
	}
	body, ok := out.Body.(map[string]any)
	if !ok {
		t.Fatalf("body = %T, want the {results:[...]} object", out.Body)
	}
	results, ok := body["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %v, want both pages aggregated", body["results"])
	}
}

func TestRetrieveBlocksFeedsMarkdown(t *testing.T) {
	// notion-retrieve-blocks leaves body.results in exactly the shape
	// notion-page-to-markdown reads by default.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","results":[` +
			`{"type":"heading_1","heading_1":{"rich_text":[{"plain_text":"Doc","annotations":{}}]}}` +
			`],"has_more":false}`))
	}))
	defer srv.Close()

	deps := blockDeps(t, srv.URL)
	fetch, err := newRetrieveBlocks(types.Settings{"connector": "notion", "block": `"p1"`}, deps)
	if err != nil {
		t.Fatalf("newRetrieveBlocks: %v", err)
	}
	render, err := newPageToMarkdown(types.Settings{}, deps)
	if err != nil {
		t.Fatalf("newPageToMarkdown: %v", err)
	}

	msg := blockMessage(t, map[string]any{})
	msg, err = fetch.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	out, err := render.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	_, md, ok := out.RawBody()
	if !ok || md != "# Doc" {
		t.Errorf("markdown = %q (ok=%v), want \"# Doc\"", md, ok)
	}
}

func TestRetrieveBlocksRequiresBlock(t *testing.T) {
	if _, err := newRetrieveBlocks(types.Settings{"connector": "notion"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when block is not set")
	}
}

func TestRetrieveBlocksStoresInResultVar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","results":[{"id":"b1"}],"has_more":false}`))
	}))
	defer srv.Close()

	proc, err := newRetrieveBlocks(types.Settings{
		"connector": "notion",
		"block":     `"p1"`,
		"resultVar": "notionBlocks",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRetrieveBlocks: %v", err)
	}
	msg := blockMessage(t, map[string]any{"keep": "me"})
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if stored, ok := out.Variables["notionBlocks"].(map[string]any); !ok || stored["results"] == nil {
		t.Errorf("notionBlocks = %v, want the {results:[...]} object", out.Variables["notionBlocks"])
	}
	if body, ok := out.Body.(map[string]any); !ok || body["keep"] != "me" {
		t.Errorf("expected the body left intact, got %v", out.Body)
	}
}
