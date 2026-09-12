package snapshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/authz"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

// memRepo is an in-memory repository enforcing the (integration_id, tag) unique
// constraint so the handler's status-code mapping can be exercised end to end.
type memRepo struct {
	snapshots map[string]Snapshot
	seq       int
	// deployed maps a snapshot id to the environment labels referencing it.
	deployed map[string][]string
	// resources maps a snapshot id to its frozen resources.
	resources map[string][]Resource
}

func newMemRepo() *memRepo {
	return &memRepo{snapshots: make(map[string]Snapshot), resources: make(map[string][]Resource)}
}

func (m *memRepo) Create(_ context.Context, integrationID, tag string) (Snapshot, error) {
	for _, s := range m.snapshots {
		if s.IntegrationID == integrationID && s.Tag == tag {
			return Snapshot{}, ErrTagExists
		}
	}
	m.seq++
	s := Snapshot{ID: idFromSeq(m.seq), IntegrationID: integrationID, Tag: tag}
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

// deployed maps a snapshot id to the environment labels referencing it; an entry
// makes DeploymentsUsingSnapshot report the snapshot as in use.
func (m *memRepo) DeploymentsUsingSnapshot(_ context.Context, _, snapshotID string) ([]string, error) {
	return m.deployed[snapshotID], nil
}

func (m *memRepo) Delete(_ context.Context, id string) error {
	if _, ok := m.snapshots[id]; !ok {
		return ErrNotFound
	}
	delete(m.snapshots, id)
	return nil
}

func (m *memRepo) ListResources(_ context.Context, snapshotID string) ([]Resource, error) {
	return m.resources[snapshotID], nil
}

func (m *memRepo) ResourceContent(_ context.Context, snapshotID, kind, name string) ([]byte, bool, error) {
	for _, res := range m.resources[snapshotID] {
		if res.Kind == kind && res.Name == name {
			return []byte(res.Content), true, nil
		}
	}
	return nil, false, nil
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

func TestHandlerFrozenResources(t *testing.T) {
	mux, repo := newTestHandler(integration.Integration{ID: "int-1", Definition: "yaml"})
	repo.resources["snap-1"] = []Resource{
		{SnapshotID: "snap-1", Kind: "env", Name: ".env.dev", Content: "GREETING=hi"},
		{SnapshotID: "snap-1", Kind: "template", Name: "templates/welcome.tmpl", Content: "hi {{name}}"},
	}

	if rec := do(mux, "GET", "/snapshots/snap-1/resources", ""); rec.Code != http.StatusOK {
		t.Errorf("list: status = %d, want 200", rec.Code)
	}

	rec := do(mux, "GET", "/snapshots/snap-1/resources/content?kind=env&name=.env.dev", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("content: status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "GREETING=hi" {
		t.Errorf("content body = %q, want the frozen bytes", rec.Body.String())
	}

	if rec := do(mux, "GET", "/snapshots/snap-1/resources/content?kind=env&name=missing", ""); rec.Code != http.StatusNotFound {
		t.Errorf("missing content: status = %d, want 404", rec.Code)
	}
	if rec := do(mux, "GET", "/snapshots/snap-1/resources/content?kind=env", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("no name: status = %d, want 400", rec.Code)
	}
}

// --- a pod reading frozen resources -----------------------------------------

// stubOwner answers which integration a deployment belongs to.
type stubOwner map[string]string

func (s stubOwner) IntegrationOf(_ context.Context, deploymentID string) (string, error) {
	return s[deploymentID], nil
}

// asDeployment drives a request carrying a machine token's principal, the way
// the authz middleware puts one on the context.
func asDeployment(mux *http.ServeMux, path, deployment string, roles ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req = req.WithContext(authz.NewContext(req.Context(), authz.Principal{
		Subject:    "user-1",
		Roles:      append([]string{authz.RoleRuntime}, roles...),
		Deployment: deployment,
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// resourcesFor stands a handler up holding one snapshot per integration, and
// returns the two snapshot ids.
func resourcesFor(t *testing.T) (*http.ServeMux, string, string) {
	t.Helper()
	repo := newMemRepo()
	mux := http.NewServeMux()
	h := NewHandler(NewService(repo, fakeIntegrations{it: integration.Integration{Definition: "yaml"}}))
	h.Register(mux)
	// dep-1 runs int-1; dep-2 runs int-2.
	h.RestrictToOwnIntegration(stubOwner{"dep-1": "int-1", "dep-2": "int-2"})

	mine, err := repo.Create(context.Background(), "int-1", "v1")
	if err != nil {
		t.Fatalf("create mine: %v", err)
	}
	theirs, err := repo.Create(context.Background(), "int-2", "v1")
	if err != nil {
		t.Fatalf("create theirs: %v", err)
	}
	return mux, mine.ID, theirs.ID
}

// platform:runtime opens these routes to every deployment without anybody
// granting it — it is how a pod loads its own definition. So the deployment on
// the token is what says which files "its own" means.
func TestAPodReadsItsOwnIntegrationsResourcesAndNoOthers(t *testing.T) {
	mux, mine, theirs := resourcesFor(t)

	if rec := asDeployment(mux, "/snapshots/"+mine+"/resources", "dep-1"); rec.Code != http.StatusOK {
		t.Errorf("a pod was refused its own resources: %d (%s)", rec.Code, rec.Body)
	}
	if rec := asDeployment(mux, "/snapshots/"+theirs+"/resources", "dep-1"); rec.Code != http.StatusForbidden {
		t.Errorf("dep-1 read int-2's resources: %d, want 403", rec.Code)
	}
	// The content route is the one the runtime actually calls, so it is checked
	// in its own right rather than assumed to follow the listing.
	content := "/snapshots/" + theirs + "/resources/content?kind=env&name=.env"
	if rec := asDeployment(mux, content, "dep-1"); rec.Code != http.StatusForbidden {
		t.Errorf("dep-1 read int-2's resource content: %d, want 403", rec.Code)
	}
}

// A deployment granted "builds integrations" holds a developer's role, and that
// grant is installation-wide by definition. Narrowing it here would take back
// what somebody ticked a box to give.
func TestAGrantedDeploymentStillReadsEveryIntegration(t *testing.T) {
	mux, _, theirs := resourcesFor(t)

	rec := asDeployment(mux, "/snapshots/"+theirs+"/resources", "dep-1", authz.RoleDeveloper)
	if rec.Code != http.StatusOK {
		t.Errorf("a deployment granted developer was refused: %d (%s)", rec.Code, rec.Body)
	}
}

// A person's token names no deployment, and nothing here applies to it.
func TestAPersonReadsAnySnapshotsResources(t *testing.T) {
	mux, _, theirs := resourcesFor(t)

	req := httptest.NewRequest("GET", "/snapshots/"+theirs+"/resources", nil)
	req = req.WithContext(authz.NewContext(req.Context(), authz.Principal{
		Subject: "user-1", Roles: []string{authz.RoleDeveloper},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("a developer was refused a snapshot's resources: %d", rec.Code)
	}
}
