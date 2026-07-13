package notion

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
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

func TestQueryDataSourceDerivesFromDatabase(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			// Retrieve database -> its data sources.
			_, _ = w.Write([]byte(`{"object":"database","id":"db1","data_sources":[{"id":"ds-from-db","name":"Tasks"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","results":[{"id":"r1"}]}`))
	}))
	defer srv.Close()

	proc, err := newQueryDataSource(types.Settings{
		"connector": "notion",
		"database":  "body.dbId",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newQueryDataSource: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"dbId": "db1"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// It first retrieves the database, then queries the derived data source.
	want := []string{"GET /databases/db1", "POST /data_sources/ds-from-db/query"}
	if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Errorf("request paths = %v, want %v", paths, want)
	}
	if _, ok := out.Variables[defaultResultsVar].(map[string]any); !ok {
		t.Errorf("%s not set", defaultResultsVar)
	}
}

func TestQueryDataSourceErrorsOnDatabaseWithoutSources(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"database","id":"db1","data_sources":[]}`))
	}))
	defer srv.Close()

	proc, err := newQueryDataSource(types.Settings{
		"connector": "notion",
		"database":  `"db1"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newQueryDataSource: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, map[string]any{})); err == nil {
		t.Error("expected an error when the database exposes no data sources")
	}
}

func TestQueryDataSourceRequiresATarget(t *testing.T) {
	if _, err := newQueryDataSource(types.Settings{"connector": "notion"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when neither dataSource nor database is set")
	}
}

func TestQueryDataSourceRejectsBothTargets(t *testing.T) {
	_, err := newQueryDataSource(types.Settings{
		"connector":  "notion",
		"dataSource": `"ds1"`,
		"database":   `"db1"`,
	}, blockDeps(t, ""))
	if err == nil {
		t.Error("expected an error when both dataSource and database are set")
	}
}
