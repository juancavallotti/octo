package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// memRepo is an in-memory repository for handler tests: it exercises the real
// Service and Handler without a database while behaving like the store.
type memRepo struct {
	items map[string]Integration
	seq   int
}

func newMemRepo() *memRepo {
	return &memRepo{items: make(map[string]Integration)}
}

func (m *memRepo) Create(_ context.Context, name, definition string) (Integration, error) {
	m.seq++
	it := Integration{ID: fmt.Sprintf("id-%d", m.seq), Name: name, Definition: definition}
	m.items[it.ID] = it
	return it, nil
}

func (m *memRepo) Get(_ context.Context, id string) (Integration, error) {
	it, ok := m.items[id]
	if !ok {
		return Integration{}, ErrNotFound
	}
	return it, nil
}

func (m *memRepo) List(_ context.Context) ([]Integration, error) {
	out := make([]Integration, 0, len(m.items))
	for _, it := range m.items {
		out = append(out, it)
	}
	return out, nil
}

func (m *memRepo) Update(_ context.Context, id, name, definition string) (Integration, error) {
	if _, ok := m.items[id]; !ok {
		return Integration{}, ErrNotFound
	}
	it := Integration{ID: id, Name: name, Definition: definition}
	m.items[id] = it
	return it, nil
}

func (m *memRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.items[id]; !ok {
		return ErrNotFound
	}
	delete(m.items, id)
	return nil
}

// newTestHandler returns a mux wired to a real Service over a memRepo, plus the
// repo so tests can seed data.
func newTestHandler(t *testing.T) (*http.ServeMux, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	mux := http.NewServeMux()
	NewHandler(NewService(repo)).Register(mux)
	return mux, repo
}

func do(t *testing.T, mux *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerCreate(t *testing.T) {
	mux, _ := newTestHandler(t)

	rec := do(t, mux, http.MethodPost, "/integrations", `{"name":"demo","definition":"body"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	var got integrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" || got.Name != "demo" || got.Definition != "body" {
		t.Errorf("unexpected response: %+v", got)
	}
}

func TestHandlerCreateBadRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{`},
		{name: "unknown field", body: `{"name":"x","extra":1}`},
		{name: "empty name", body: `{"name":"  ","definition":"b"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, _ := newTestHandler(t)
			rec := do(t, mux, http.MethodPost, "/integrations", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body)
			}
		})
	}
}

func TestHandlerGet(t *testing.T) {
	mux, repo := newTestHandler(t)
	seeded, _ := repo.Create(context.Background(), "seeded", "body")

	rec := do(t, mux, http.MethodGet, "/integrations/"+seeded.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	missing := do(t, mux, http.MethodGet, "/integrations/nope", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status = %d, want 404", missing.Code)
	}
}

func TestHandlerList(t *testing.T) {
	mux, repo := newTestHandler(t)
	_, _ = repo.Create(context.Background(), "a", "b")
	_, _ = repo.Create(context.Background(), "c", "d")

	rec := do(t, mux, http.MethodGet, "/integrations", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []integrationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestHandlerUpdate(t *testing.T) {
	mux, repo := newTestHandler(t)
	seeded, _ := repo.Create(context.Background(), "before", "body")

	rec := do(t, mux, http.MethodPut, "/integrations/"+seeded.ID, `{"name":"after","definition":"body2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	var got integrationResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "after" || got.Definition != "body2" {
		t.Errorf("unexpected response: %+v", got)
	}

	missing := do(t, mux, http.MethodPut, "/integrations/nope", `{"name":"x","definition":"y"}`)
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status = %d, want 404", missing.Code)
	}
}

func TestHandlerDelete(t *testing.T) {
	mux, repo := newTestHandler(t)
	seeded, _ := repo.Create(context.Background(), "del", "body")

	rec := do(t, mux, http.MethodDelete, "/integrations/"+seeded.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	missing := do(t, mux, http.MethodDelete, "/integrations/nope", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status = %d, want 404", missing.Code)
	}
}
