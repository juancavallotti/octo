package snapshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

// memRepo is an in-memory repository enforcing the (integration_id, tag) unique
// constraint so the handler's status-code mapping can be exercised end to end.
type memRepo struct {
	snapshots map[string]Snapshot
	seq       int
}

func newMemRepo() *memRepo { return &memRepo{snapshots: make(map[string]Snapshot)} }

func (m *memRepo) Create(_ context.Context, integrationID, tag, definition string) (Snapshot, error) {
	for _, s := range m.snapshots {
		if s.IntegrationID == integrationID && s.Tag == tag {
			return Snapshot{}, ErrTagExists
		}
	}
	m.seq++
	s := Snapshot{ID: idFromSeq(m.seq), IntegrationID: integrationID, Tag: tag, Definition: definition}
	m.snapshots[s.ID] = s
	return s, nil
}

func (m *memRepo) Get(_ context.Context, id string) (Snapshot, error) {
	s, ok := m.snapshots[id]
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	return s, nil
}

func (m *memRepo) ListByIntegration(_ context.Context, integrationID string) ([]Snapshot, error) {
	out := make([]Snapshot, 0)
	for _, s := range m.snapshots {
		if s.IntegrationID == integrationID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.snapshots[id]; !ok {
		return ErrNotFound
	}
	delete(m.snapshots, id)
	return nil
}

func idFromSeq(n int) string { return "snap-" + string(rune('0'+n)) }

func newTestHandler(it integration.Integration) (*http.ServeMux, *memRepo) {
	repo := newMemRepo()
	mux := http.NewServeMux()
	NewHandler(NewService(repo, fakeIntegrations{it: it})).Register(mux)
	return mux, repo
}

func do(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerCreateAndConflict(t *testing.T) {
	mux, _ := newTestHandler(integration.Integration{ID: "int-1", Definition: "yaml"})

	if rec := do(mux, "POST", "/integrations/int-1/snapshots", `{"tag":"v1"}`); rec.Code != http.StatusCreated {
		t.Fatalf("first create: status = %d, want 201 (%s)", rec.Code, rec.Body)
	}
	if rec := do(mux, "POST", "/integrations/int-1/snapshots", `{"tag":"v1"}`); rec.Code != http.StatusConflict {
		t.Errorf("duplicate tag: status = %d, want 409", rec.Code)
	}
	if rec := do(mux, "POST", "/integrations/int-1/snapshots", `{"tag":"bad tag"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid tag: status = %d, want 400", rec.Code)
	}
}

func TestHandlerListAndDelete(t *testing.T) {
	mux, _ := newTestHandler(integration.Integration{ID: "int-1", Definition: "yaml"})
	do(mux, "POST", "/integrations/int-1/snapshots", `{"tag":"v1"}`)

	if rec := do(mux, "GET", "/integrations/int-1/snapshots", ""); rec.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", rec.Code)
	}
	if rec := do(mux, "DELETE", "/snapshots/snap-1", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: status = %d, want 204", rec.Code)
	}
	if rec := do(mux, "DELETE", "/snapshots/missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing: status = %d, want 404", rec.Code)
	}
}
