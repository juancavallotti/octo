package folder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

// memRepo is an in-memory repository for handler tests: it exercises the real
// Service and Handler without a database while behaving like the store
// (single-folder membership, cascade-on-delete, name-ordered listing).
type memRepo struct {
	folders      map[string]Folder
	members      map[string]string                  // integrationID -> folderID
	integrations map[string]integration.Integration // seeded integrations
	seq          int
}

func newMemRepo() *memRepo {
	return &memRepo{
		folders:      make(map[string]Folder),
		members:      make(map[string]string),
		integrations: make(map[string]integration.Integration),
	}
}

func (m *memRepo) Create(_ context.Context, name string, parentID *string) (Folder, error) {
	if parentID != nil {
		if _, ok := m.folders[*parentID]; !ok {
			return Folder{}, ErrNotFound
		}
	}
	m.seq++
	f := Folder{ID: fmt.Sprintf("f-%d", m.seq), Name: name, ParentID: parentID}
	m.folders[f.ID] = f
	return f, nil
}

func (m *memRepo) Get(_ context.Context, id string) (Folder, error) {
	f, ok := m.folders[id]
	if !ok {
		return Folder{}, ErrNotFound
	}
	return f, nil
}

func (m *memRepo) List(_ context.Context) ([]Folder, error) {
	out := make([]Folder, 0, len(m.folders))
	for _, f := range m.folders {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *memRepo) Update(_ context.Context, id, name string, parentID *string) (Folder, error) {
	if _, ok := m.folders[id]; !ok {
		return Folder{}, ErrNotFound
	}
	if parentID != nil {
		if _, ok := m.folders[*parentID]; !ok {
			return Folder{}, ErrNotFound
		}
	}
	f := Folder{ID: id, Name: name, ParentID: parentID}
	m.folders[id] = f
	return f, nil
}

func (m *memRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.folders[id]; !ok {
		return ErrNotFound
	}
	delete(m.folders, id)
	for intID, folderID := range m.members {
		if folderID == id {
			delete(m.members, intID)
		}
	}
	return nil
}

func (m *memRepo) AddIntegration(_ context.Context, folderID, integrationID string) error {
	if _, ok := m.folders[folderID]; !ok {
		return ErrNotFound
	}
	if _, ok := m.integrations[integrationID]; !ok {
		return ErrNotFound
	}
	m.members[integrationID] = folderID
	return nil
}

func (m *memRepo) RemoveIntegration(_ context.Context, folderID, integrationID string) error {
	if m.members[integrationID] != folderID {
		return ErrNotFound
	}
	delete(m.members, integrationID)
	return nil
}

func (m *memRepo) ListIntegrations(_ context.Context, folderID string) ([]integration.Integration, error) {
	out := make([]integration.Integration, 0)
	for intID, fID := range m.members {
		if fID == folderID {
			out = append(out, m.integrations[intID])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// seedIntegration registers an integration so membership operations can target
// it, mimicking a row created via the integrations API.
func (m *memRepo) seedIntegration(id, name string) {
	m.integrations[id] = integration.Integration{ID: id, Name: name}
}

func newTestHandler() (*http.ServeMux, *memRepo) {
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
	mux, _ := newTestHandler()

	rec := do(t, mux, http.MethodPost, "/folders", `{"name":"root"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	var got folderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" || got.Name != "root" || got.ParentID != nil {
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
		{name: "empty name", body: `{"name":"  "}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, _ := newTestHandler()
			rec := do(t, mux, http.MethodPost, "/folders", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body)
			}
		})
	}
}

func TestHandlerCreateNestedUnderMissingParent(t *testing.T) {
	mux, _ := newTestHandler()
	rec := do(t, mux, http.MethodPost, "/folders", `{"name":"child","parentId":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body)
	}
}

func TestHandlerGet(t *testing.T) {
	mux, repo := newTestHandler()
	seeded, _ := repo.Create(context.Background(), "seeded", nil)

	rec := do(t, mux, http.MethodGet, "/folders/"+seeded.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	missing := do(t, mux, http.MethodGet, "/folders/nope", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status = %d, want 404", missing.Code)
	}
}

func TestHandlerListTree(t *testing.T) {
	mux, repo := newTestHandler()
	root, _ := repo.Create(context.Background(), "root", nil)
	_, _ = repo.Create(context.Background(), "child", &root.ID)

	rec := do(t, mux, http.MethodGet, "/folders", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []folderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("roots = %d, want 1", len(got))
	}
	if len(got[0].Children) != 1 || got[0].Children[0].Name != "child" {
		t.Errorf("tree = %+v, want root with one child", got[0])
	}
}

func TestHandlerUpdate(t *testing.T) {
	mux, repo := newTestHandler()
	seeded, _ := repo.Create(context.Background(), "before", nil)

	rec := do(t, mux, http.MethodPut, "/folders/"+seeded.ID, `{"name":"after"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	var got folderResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "after" {
		t.Errorf("name = %q, want after", got.Name)
	}

	missing := do(t, mux, http.MethodPut, "/folders/nope", `{"name":"x"}`)
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status = %d, want 404", missing.Code)
	}
}

func TestHandlerUpdateCycleRejected(t *testing.T) {
	mux, repo := newTestHandler()
	root, _ := repo.Create(context.Background(), "root", nil)
	child, _ := repo.Create(context.Background(), "child", &root.ID)

	// Move root under its child -> cycle -> 400.
	rec := do(t, mux, http.MethodPut, "/folders/"+root.ID,
		fmt.Sprintf(`{"name":"root","parentId":%q}`, child.ID))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
}

func TestHandlerDelete(t *testing.T) {
	mux, repo := newTestHandler()
	seeded, _ := repo.Create(context.Background(), "del", nil)

	rec := do(t, mux, http.MethodDelete, "/folders/"+seeded.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	missing := do(t, mux, http.MethodDelete, "/folders/nope", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing status = %d, want 404", missing.Code)
	}
}

func TestHandlerMembership(t *testing.T) {
	mux, repo := newTestHandler()
	root, _ := repo.Create(context.Background(), "root", nil)
	repo.seedIntegration("int-1", "alpha")

	// Add (204).
	add := do(t, mux, http.MethodPut, "/folders/"+root.ID+"/integrations/int-1", "")
	if add.Code != http.StatusNoContent {
		t.Fatalf("add status = %d, want 204; body=%s", add.Code, add.Body)
	}

	// List (200, one member).
	list := do(t, mux, http.MethodGet, "/folders/"+root.ID+"/integrations", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", list.Code)
	}
	var members []integrationResponse
	if err := json.Unmarshal(list.Body.Bytes(), &members); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(members) != 1 || members[0].ID != "int-1" {
		t.Errorf("members = %+v, want just int-1", members)
	}

	// Remove (204), then removing again is 404.
	rm := do(t, mux, http.MethodDelete, "/folders/"+root.ID+"/integrations/int-1", "")
	if rm.Code != http.StatusNoContent {
		t.Errorf("remove status = %d, want 204", rm.Code)
	}
	rmAgain := do(t, mux, http.MethodDelete, "/folders/"+root.ID+"/integrations/int-1", "")
	if rmAgain.Code != http.StatusNotFound {
		t.Errorf("second remove status = %d, want 404", rmAgain.Code)
	}
}

func TestHandlerAddIntegrationMissingFolder(t *testing.T) {
	mux, repo := newTestHandler()
	repo.seedIntegration("int-1", "alpha")

	rec := do(t, mux, http.MethodPut, "/folders/nope/integrations/int-1", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body=%s", rec.Code, rec.Body)
	}
}
