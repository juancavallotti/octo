package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/types"
)

func TestQueryDataSourceBuildsBodyAndFoldsResult(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","results":[{"id":"r1"}],"has_more":false}`))
	}))
	defer srv.Close()

	proc, err := newQueryDataSource(types.Settings{
		"connector":  "notion",
		"dataSource": "body.dsId",
		"filter":     `{"property": "Status", "status": {"equals": "Done"}}`,
		"sorts":      `[{"timestamp": "created_time", "direction": "descending"}]`,
		"pageSize":   10,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newQueryDataSource: %v", err)
	}

	msg := blockMessage(t, map[string]any{"dsId": "d1"})
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if gotPath != "/data_sources/d1/query" {
		t.Errorf("path = %q, want /data_sources/d1/query", gotPath)
	}
	if gotBody["page_size"] != float64(10) {
		t.Errorf("page_size = %v, want 10", gotBody["page_size"])
	}
	filter, ok := gotBody["filter"].(map[string]any)
	if !ok || filter["property"] != "Status" {
		t.Errorf("filter = %v", gotBody["filter"])
	}
	if _, ok := gotBody["sorts"].([]any); !ok {
		t.Errorf("sorts = %v, want a list", gotBody["sorts"])
	}
	result, ok := out.Variables[defaultResultsVar].(map[string]any)
	if !ok || result["object"] != "list" {
		t.Errorf("%s = %v, want the query response", defaultResultsVar, out.Variables[defaultResultsVar])
	}
}

func TestQueryDataSourceOmitsUnsetOptionals(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"object":"list","results":[]}`))
	}))
	defer srv.Close()

	proc, err := newQueryDataSource(types.Settings{
		"connector":  "notion",
		"dataSource": `"d1"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newQueryDataSource: %v", err)
	}

	if _, err := proc.Process(context.Background(), blockMessage(t, map[string]any{})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if _, ok := gotBody["filter"]; ok {
		t.Error("did not expect a filter in the body when unset")
	}
	if _, ok := gotBody["sorts"]; ok {
		t.Error("did not expect sorts in the body when unset")
	}
	if _, ok := gotBody["page_size"]; ok {
		t.Error("did not expect page_size in the body when unset")
	}
}

func TestQueryDataSourceRequiresDataSource(t *testing.T) {
	if _, err := newQueryDataSource(types.Settings{"connector": "notion"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when dataSource is not set")
	}
}
