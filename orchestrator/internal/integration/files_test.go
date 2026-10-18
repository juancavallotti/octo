package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
)

func (m *memRepo) ConfigFiles(_ context.Context, integrationID string) ([]ConfigFile, error) {
	if _, ok := m.items[integrationID]; !ok {
		return nil, ErrNotFound
	}
	ret := []ConfigFile{}
	for _, f := range m.files[integrationID] {
		ret = append(ret, f)
	}
	sort.Slice(ret, func(i, j int) bool { return ret[i].Path < ret[j].Path })
	return ret, nil
}

func (m *memRepo) PutConfigFile(_ context.Context, integrationID, path, content, _ string) (ConfigFile, error) {
	if _, ok := m.items[integrationID]; !ok {
		return ConfigFile{}, ErrNotFound
	}
	if m.files[integrationID] == nil {
		m.files[integrationID] = map[string]ConfigFile{}
	}
	f := ConfigFile{Path: path, Content: content}
	m.files[integrationID][path] = f
	return f, nil
}

func (m *memRepo) DeleteConfigFile(_ context.Context, integrationID, path, _ string) error {
	if _, ok := m.items[integrationID]; !ok {
		return ErrNotFound
	}
	if _, ok := m.files[integrationID][path]; !ok {
		return ErrFileNotFound
	}
	delete(m.files[integrationID], path)
	return nil
}

// fileURL addresses one config file of an integration.
func fileURL(integrationID, path string) string {
	return "/integrations/" + integrationID + "/files/content?path=" + url.QueryEscape(path)
}

func TestValidatePath(t *testing.T) {
	tests := []struct {
		path string
		ok   bool
	}{
		{path: "orders.yaml", ok: true},
		{path: "orders.YML", ok: true},
		{path: "", ok: false},
		{path: "flows/orders.yaml", ok: false},
		{path: "orders_test.yaml", ok: false},
		{path: ".env.dev", ok: false},
		{path: "welcome.tmpl", ok: false},
		{path: `a\b.yaml`, ok: false},
		{path: strings.Repeat("a", maxPathLen) + ".yaml", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := validatePath(tt.path)
			if tt.ok && err != nil {
				t.Errorf("validatePath(%q) = %v, want ok", tt.path, err)
			}
			if !tt.ok && !errors.Is(err, ErrInvalid) {
				t.Errorf("validatePath(%q) = %v, want ErrInvalid", tt.path, err)
			}
		})
	}
}

func TestHandlerFileLifecycle(t *testing.T) {
	mux, repo := newTestHandler(t)
	seeded, _ := repo.Create(context.Background(), "files", "", "")

	if rec := do(t, mux, http.MethodPut, fileURL(seeded.ID, "a.yaml"),
		`{"content":"flows:\n  - name: intake\n"}`); rec.Code != http.StatusOK {
		t.Fatalf("put a: status = %d; body=%s", rec.Code, rec.Body)
	}
	if rec := do(t, mux, http.MethodPut, fileURL(seeded.ID, "b.yaml"),
		`{"content":"flows:\n  - name: dispatch\n"}`); rec.Code != http.StatusOK {
		t.Fatalf("put b: status = %d; body=%s", rec.Code, rec.Body)
	}

	rec := do(t, mux, http.MethodGet, "/integrations/"+seeded.ID+"/files", "")
	var listed []fileSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed) != 2 || listed[0].Path != "a.yaml" || listed[1].Path != "b.yaml" {
		t.Errorf("listed = %+v, want a.yaml then b.yaml", listed)
	}

	rec = do(t, mux, http.MethodGet, fileURL(seeded.ID, "b.yaml"), "")
	var got fileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if !strings.Contains(got.Content, "dispatch") {
		t.Errorf("get b.yaml content = %q", got.Content)
	}

	if rec := do(t, mux, http.MethodDelete, fileURL(seeded.ID, "b.yaml"), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d; body=%s", rec.Code, rec.Body)
	}
	if rec := do(t, mux, http.MethodGet, fileURL(seeded.ID, "b.yaml"), ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: status = %d, want 404", rec.Code)
	}
}

// A write that leaves the project unable to merge would make the integration
// unreadable, so it is refused and the stored files stay as they were.
func TestHandlerRefusesAFileThatDoesNotMerge(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "duplicate flow", content: "flows:\n  - name: intake\n"},
		{name: "second service", content: "service:\n  name: other\n"},
		{name: "not a mapping", content: "- just\n- a list\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, repo := newTestHandler(t)
			seeded, _ := repo.Create(context.Background(), "conflicts", "", "")
			_, _ = repo.PutConfigFile(context.Background(), seeded.ID, "a.yaml",
				"service:\n  name: orders\nflows:\n  - name: intake\n", "")

			body, _ := json.Marshal(fileRequest{Content: tt.content})
			rec := do(t, mux, http.MethodPut, fileURL(seeded.ID, "b.yaml"), string(body))
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body)
			}
			if _, stored := repo.files[seeded.ID]["b.yaml"]; stored {
				t.Error("a refused write was stored")
			}
		})
	}
}

// Rewriting the file that already holds a name is an edit, not a duplicate.
func TestHandlerRewritesAFileInPlace(t *testing.T) {
	mux, repo := newTestHandler(t)
	seeded, _ := repo.Create(context.Background(), "rewrite", "", "")
	_, _ = repo.PutConfigFile(context.Background(), seeded.ID, "a.yaml", "flows:\n  - name: intake\n", "")
	_, _ = repo.PutConfigFile(context.Background(), seeded.ID, "b.yaml", "flows:\n  - name: dispatch\n", "")

	rec := do(t, mux, http.MethodPut, fileURL(seeded.ID, "a.yaml"),
		`{"content":"flows:\n  - name: intake\n  - name: audit\n"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
}

func TestHandlerFileErrors(t *testing.T) {
	mux, repo := newTestHandler(t)
	seeded, _ := repo.Create(context.Background(), "errors", "", "")

	tests := []struct {
		name   string
		method string
		target string
		body   string
		want   int
	}{
		{name: "nested path", method: http.MethodPut, target: fileURL(seeded.ID, "flows/a.yaml"), body: `{"content":""}`, want: http.StatusBadRequest},
		{name: "test suite", method: http.MethodPut, target: fileURL(seeded.ID, "a_test.yaml"), body: `{"content":""}`, want: http.StatusBadRequest},
		{name: "resource", method: http.MethodGet, target: fileURL(seeded.ID, ".env"), want: http.StatusBadRequest},
		{name: "no path", method: http.MethodDelete, target: "/integrations/" + seeded.ID + "/files/content", want: http.StatusBadRequest},
		{name: "missing file", method: http.MethodGet, target: fileURL(seeded.ID, "nope.yaml"), want: http.StatusNotFound},
		{name: "missing file delete", method: http.MethodDelete, target: fileURL(seeded.ID, "nope.yaml"), want: http.StatusNotFound},
		{name: "unknown integration list", method: http.MethodGet, target: "/integrations/nope/files", want: http.StatusNotFound},
		{name: "unknown integration put", method: http.MethodPut, target: fileURL("nope", "a.yaml"), body: `{"content":""}`, want: http.StatusNotFound},
		{name: "malformed body", method: http.MethodPut, target: fileURL(seeded.ID, "a.yaml"), body: `{`, want: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(t, mux, tt.method, tt.target, tt.body); rec.Code != tt.want {
				t.Errorf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body)
			}
		})
	}
}
