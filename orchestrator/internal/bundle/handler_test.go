package bundle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// newTestServer wires the handler over the same fakes the service tests use,
// returning the mux and the fakes so a test can seed and inspect them.
func newTestServer() (http.Handler, *fakeIntegrations, *fakeResources, *fakeSnapshots) {
	svc, ints, res, snaps := newService()
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	return mux, ints, res, snaps
}

func TestExportServesADownloadableArchive(t *testing.T) {
	mux, ints, res, _ := newTestServer()
	ctx := context.Background()
	it, _ := ints.Create(ctx, "Order Sync", "name: order-sync\n", "")
	if _, err := res.Create(ctx, it.ID, "env", ".env.dev", "A=1\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/integrations/"+it.ID+"/bundle", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != archiveContentType {
		t.Errorf("Content-Type = %q, want %q", got, archiveContentType)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="order-sync.zip"`) {
		t.Errorf("Content-Disposition = %q, want the integration's slug", got)
	}
	b, err := Read(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("the response is not a readable bundle: %v", err)
	}
	if b.Name != "Order Sync" || len(b.Resources) != 1 {
		t.Errorf("bundle = %+v", b)
	}
}

func TestExportSnapshotNamesTheDownloadAfterTheTag(t *testing.T) {
	mux, ints, _, snaps := newTestServer()
	it, _ := ints.Create(context.Background(), "Order Sync", "definition: live\n", "")
	snaps.snaps["s1"] = snapshot.Snapshot{ID: "s1", IntegrationID: it.ID, Tag: "v1.2", Definition: "definition: frozen\n"}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/snapshots/s1/bundle", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="order-sync-v1-2.zip"`) {
		t.Errorf("Content-Disposition = %q, want the tag in the filename", got)
	}
}

func TestExportUnknownIntegrationIs404(t *testing.T) {
	mux, _, _, _ := newTestServer()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/integrations/nope/bundle", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestImportCreatesAnIntegration(t *testing.T) {
	mux, ints, res, _ := newTestServer()
	data, err := Write(Bundle{
		Name:       "Order Sync",
		Definition: "name: order-sync\n",
		Resources:  []File{{Kind: kindEnv, Name: ".env.dev", Content: "A=1\n"}},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/integrations/bundle?name=ignored", bytes.NewReader(data))
	req.Header.Set("Content-Type", archiveContentType)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got integrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Order Sync" {
		t.Errorf("name = %q, want the manifest's", got.Name)
	}
	if _, ok := ints.items[got.ID]; !ok {
		t.Errorf("integration %q was not stored", got.ID)
	}
	if names := namesOf(t, res, got.ID); len(names) != 1 || names[0] != ".env.dev" {
		t.Errorf("resources = %v, want [.env.dev]", names)
	}
}

// The `name` query parameter is what names a manifest-less archive.
func TestImportTakesItsNameFromTheQueryWhenTheArchiveIsSilent(t *testing.T) {
	mux, _, _, _ := newTestServer()
	data := zipOf(t, map[string]string{"whatever.yaml": "a: b\n"})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/integrations/bundle?name=From+A+File", bytes.NewReader(data)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got integrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "From A File" {
		t.Errorf("name = %q, want the query's", got.Name)
	}
}

func TestImportRejectsSomethingThatIsNotABundle(t *testing.T) {
	mux, _, _, _ := newTestServer()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/integrations/bundle", strings.NewReader("name: plain-yaml\n")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body = %s)", rec.Code, rec.Body.String())
	}
}

func TestImportRejectsAnOversizeUpload(t *testing.T) {
	mux, _, _, _ := newTestServer()
	rec := httptest.NewRecorder()
	body := strings.NewReader(strings.Repeat("x", maxUploadBytes+1))
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/integrations/bundle", body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body = %s)", rec.Code, rec.Body.String())
	}
}

func TestReplaceOverwritesTheIntegration(t *testing.T) {
	mux, ints, res, _ := newTestServer()
	ctx := context.Background()
	it, _ := ints.Create(ctx, "Order Sync", "definition: old\n", "")
	if _, err := res.Create(ctx, it.ID, "template", "templates/gone.tmpl", "bye\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	data, err := Write(Bundle{Name: "Whatever", Definition: "definition: new\n"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/integrations/"+it.ID+"/bundle", bytes.NewReader(data)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	stored := ints.items[it.ID]
	if stored.Definition != "definition: new\n" || stored.Name != "Order Sync" {
		t.Errorf("stored = %+v, want the new definition under the old name", stored)
	}
	if names := namesOf(t, res, it.ID); len(names) != 0 {
		t.Errorf("resources = %v, want the dropped ones removed", names)
	}
}

func TestReplaceUnknownIntegrationIs404(t *testing.T) {
	mux, _, _, _ := newTestServer()
	data, err := Write(Bundle{Name: "x", Definition: "a: b\n"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/integrations/nope/bundle", bytes.NewReader(data)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// A name conflict is something the caller can fix by renaming, so it answers 409
// rather than the 500 an unmapped domain error would produce.
func TestImportReportsANameItCannotFreeAsAConflict(t *testing.T) {
	mux, ints, _, _ := newTestServer()
	ctx := context.Background()
	data, err := Write(Bundle{Name: "Order Sync", Definition: "a: b\n"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Every suffix this import would try is already taken.
	if _, err := ints.Create(ctx, "Order Sync", "a: b\n", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for i := 2; i <= nameAttempts+1; i++ {
		if _, err := ints.Create(ctx, fmt.Sprintf("Order Sync (%d)", i), "a: b\n", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/integrations/bundle", bytes.NewReader(data)))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body = %s)", rec.Code, rec.Body.String())
	}
}
