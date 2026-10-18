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

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
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

func (m *memRepo) ReplaceConfigFiles(_ context.Context, integrationID string, files []projectfile.File, _ string) error {
	if _, ok := m.items[integrationID]; !ok {
		return ErrNotFound
	}
	m.files[integrationID] = map[string]ConfigFile{}
	for _, f := range files {
		m.files[integrationID][f.Path] = ConfigFile{Path: f.Path, Content: f.Content}
	}
	return nil
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

// Replacing the whole project at once lets a flow move between files, which a
// file-by-file write could only do by passing through a duplicate.
func TestReplaceFilesMovesAFlowBetweenFiles(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()
	seeded, _ := repo.Create(ctx, "move", "", "")
	_, _ = repo.PutConfigFile(ctx, seeded.ID, "a.yaml", "flows:\n  - name: intake\n", "")
	_, _ = repo.PutConfigFile(ctx, seeded.ID, "stale.yaml", "flows: []\n", "")

	err := svc.ReplaceFiles(ctx, seeded.ID, []projectfile.File{
		{Path: "a.yaml", Content: "flows: []\n"},
		{Path: "b.yaml", Content: "flows:\n  - name: intake\n"},
	}, "")
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	files, _ := repo.ConfigFiles(ctx, seeded.ID)
	if len(files) != 2 || files[0].Path != "a.yaml" || files[1].Path != "b.yaml" {
		t.Errorf("files = %+v, want a.yaml and b.yaml only", files)
	}
}

func TestReplaceFilesRefusals(t *testing.T) {
	tests := []struct {
		name  string
		files []projectfile.File
		want  error
	}{
		{name: "not config", files: []projectfile.File{{Path: "templates/a.yaml"}}, want: ErrInvalid},
		{name: "path twice", files: []projectfile.File{{Path: "a.yaml"}, {Path: "a.yaml"}}, want: ErrInvalid},
		{name: "conflict", files: []projectfile.File{
			{Path: "a.yaml", Content: "flows:\n  - name: intake\n"},
			{Path: "b.yaml", Content: "flows:\n  - name: intake\n"},
		}, want: ErrFilesConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMemRepo()
			seeded, _ := repo.Create(context.Background(), "refusals", "", "")
			err := NewService(repo).ReplaceFiles(context.Background(), seeded.ID, tt.files, "")
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

// The whole set goes in one request, so a flow can move between files without
// passing through the duplicate a file-by-file save would be refused for.
func TestHandlerReplacesTheWholeFileSet(t *testing.T) {
	mux, repo := newTestHandler(t)
	ctx := context.Background()
	seeded, _ := repo.Create(ctx, "replace-set", "", "")
	_, _ = repo.PutConfigFile(ctx, seeded.ID, "a.yaml", "flows:\n  - name: intake\n", "")
	_, _ = repo.PutConfigFile(ctx, seeded.ID, "stale.yaml", "flows: []\n", "")

	rec := do(t, mux, http.MethodPut, "/integrations/"+seeded.ID+"/files", `{"files":[
		{"path":"a.yaml","content":"flows: []\n"},
		{"path":"b.yaml","content":"flows:\n  - name: intake\n"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	var listed []fileSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed) != 2 || listed[0].Path != "a.yaml" || listed[1].Path != "b.yaml" {
		t.Errorf("listed = %+v, want a.yaml and b.yaml only", listed)
	}
	if !strings.Contains(repo.files[seeded.ID]["b.yaml"].Content, "intake") {
		t.Errorf("b.yaml = %q, want the moved flow", repo.files[seeded.ID]["b.yaml"].Content)
	}
}

func TestHandlerReplaceFilesErrors(t *testing.T) {
	mux, repo := newTestHandler(t)
	seeded, _ := repo.Create(context.Background(), "replace-errors", "", "")
	_, _ = repo.PutConfigFile(context.Background(), seeded.ID, "a.yaml", "flows: []\n", "")
	target := "/integrations/" + seeded.ID + "/files"

	tests := []struct {
		name   string
		target string
		body   string
		want   int
	}{
		{name: "conflict", target: target, body: `{"files":[{"path":"a.yaml","content":"flows:\n  - name: x\n"},{"path":"b.yaml","content":"flows:\n  - name: x\n"}]}`, want: http.StatusConflict},
		{name: "not config", target: target, body: `{"files":[{"path":"flows/a.yaml","content":""}]}`, want: http.StatusBadRequest},
		{name: "path twice", target: target, body: `{"files":[{"path":"a.yaml","content":""},{"path":"a.yaml","content":""}]}`, want: http.StatusBadRequest},
		{name: "malformed body", target: target, body: `{`, want: http.StatusBadRequest},
		{name: "unknown integration", target: "/integrations/nope/files", body: `{"files":[]}`, want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if rec := do(t, mux, http.MethodPut, tt.target, tt.body); rec.Code != tt.want {
				t.Errorf("status = %d, want %d; body=%s", rec.Code, tt.want, rec.Body)
			}
		})
	}
	if got := repo.files[seeded.ID]["a.yaml"].Content; got != "flows: []\n" {
		t.Errorf("a refused replace changed a.yaml to %q", got)
	}
}
