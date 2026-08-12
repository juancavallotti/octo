package openapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve runs one request against a mux with the routes registered.
func serve(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler().Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestDocumentServesTheSpecAsJSON(t *testing.T) {
	rec := serve(t, "/openapi.json")

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("want a JSON content type, got %q", ct)
	}

	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("the served document does not parse: %v", err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Errorf("want an OpenAPI 3.1 document, got version %v", doc["openapi"])
	}
}

func TestDocumentFiltersByTag(t *testing.T) {
	rec := serve(t, "/openapi.json?tag=integrations")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	paths, _ := decode(t, rec.Body.Bytes())
	if len(paths) == 0 {
		t.Fatal("want the integrations routes kept")
	}
	for path := range paths {
		if !strings.HasPrefix(path, "/integrations") {
			t.Errorf("want only integration routes, got %q", path)
		}
	}
}

// A filtered document should carry less than the whole one — that is the entire
// reason a caller asks for one.
//
// Counted structurally rather than in bytes: the unfiltered document is returned
// verbatim and the generator indents it, so a byte comparison would pass on
// whitespace alone even if the filter removed nothing.
func TestFilteredDocumentCarriesLessThanTheFullOne(t *testing.T) {
	fullPaths, fullSchemas := decode(t, serve(t, "/openapi.json").Body.Bytes())
	keptPaths, keptSchemas := decode(t, serve(t, "/openapi.json?tag=integrations").Body.Bytes())

	if len(keptPaths) >= len(fullPaths) {
		t.Errorf("want fewer paths after filtering, got %d of %d", len(keptPaths), len(fullPaths))
	}
	if len(keptSchemas) >= len(fullSchemas) {
		t.Errorf("want fewer schemas after filtering, got %d of %d", len(keptSchemas), len(fullSchemas))
	}
}

// The two filters are alternatives. Silently preferring one would answer a
// different question than the caller asked.
func TestDocumentRejectsBothFiltersAtOnce(t *testing.T) {
	rec := serve(t, "/openapi.json?tag=integrations&path=/deployments")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not both") {
		t.Errorf("want the message to say the filters are alternatives, got %s", rec.Body.String())
	}
}

func TestOperationsServesTheIndex(t *testing.T) {
	rec := serve(t, "/openapi/operations")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var index []Operation
	if err := json.Unmarshal(rec.Body.Bytes(), &index); err != nil {
		t.Fatalf("the served index does not parse: %v", err)
	}
	if len(index) == 0 {
		t.Fatal("want a non-empty index")
	}
	for _, op := range index {
		if op.Method == "" || op.Path == "" {
			t.Fatalf("every entry needs a method and a path, got %+v", op)
		}
	}
}

// The index earns its existence by being cheap: a caller reads it to decide what to
// ask for, and it is worthless if that costs the same as reading everything.
//
// Compared against the document re-encoded compactly, so the generator's
// indentation is not doing the work.
func TestOperationsIsFarSmallerThanTheDocument(t *testing.T) {
	var doc any
	if err := json.Unmarshal(serve(t, "/openapi.json").Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal document: %v", err)
	}
	compact, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-encode document: %v", err)
	}

	index := serve(t, "/openapi/operations").Body.Len()
	if index*2 >= len(compact) {
		t.Errorf("want the index to be a small fraction of the document, got %d against %d", index, len(compact))
	}
}
