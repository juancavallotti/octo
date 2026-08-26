package bundle

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// fakeIntegrations is an in-memory stand-in for the integration service: enough
// of it to create, read, replace and delete, with the name-uniqueness rule the
// import path depends on.
type fakeIntegrations struct {
	items  map[string]integration.Integration
	nextID int
	// failCreate makes the next Create fail, for the rollback test.
	failCreate error
}

func newFakeIntegrations() *fakeIntegrations {
	return &fakeIntegrations{items: map[string]integration.Integration{}}
}

func (f *fakeIntegrations) Get(_ context.Context, id string) (integration.Integration, error) {
	it, ok := f.items[id]
	if !ok {
		return integration.Integration{}, integration.ErrNotFound
	}
	return it, nil
}

func (f *fakeIntegrations) Create(_ context.Context, name, definition, _ string) (integration.Integration, error) {
	if f.failCreate != nil {
		return integration.Integration{}, f.failCreate
	}
	for _, it := range f.items {
		if strings.EqualFold(it.Name, name) {
			return integration.Integration{}, integration.ErrNameTaken
		}
	}
	f.nextID++
	it := integration.Integration{ID: fmt.Sprintf("i%d", f.nextID), Name: name, Definition: definition}
	f.items[it.ID] = it
	return it, nil
}

func (f *fakeIntegrations) Update(_ context.Context, id, name, definition, _ string) (integration.Integration, error) {
	it, ok := f.items[id]
	if !ok {
		return integration.Integration{}, integration.ErrNotFound
	}
	it.Name, it.Definition = name, definition
	f.items[id] = it
	return it, nil
}

func (f *fakeIntegrations) Delete(_ context.Context, id string) error {
	delete(f.items, id)
	return nil
}

// fakeResources is an in-memory stand-in for the resource service, keyed by
// integration.
type fakeResources struct {
	items  map[string][]resource.Resource
	nextID int
	// failCreate makes every Create fail, for the rollback test.
	failCreate error
}

func newFakeResources() *fakeResources {
	return &fakeResources{items: map[string][]resource.Resource{}}
}

func (f *fakeResources) ListByIntegration(_ context.Context, integrationID string) ([]resource.Resource, error) {
	return f.items[integrationID], nil
}

func (f *fakeResources) Create(_ context.Context, integrationID, kind, name, content string) (resource.Resource, error) {
	if f.failCreate != nil {
		return resource.Resource{}, f.failCreate
	}
	f.nextID++
	r := resource.Resource{
		ID:            fmt.Sprintf("r%d", f.nextID),
		IntegrationID: integrationID,
		Kind:          kind,
		Name:          name,
		Content:       content,
	}
	f.items[integrationID] = append(f.items[integrationID], r)
	return r, nil
}

func (f *fakeResources) Update(_ context.Context, integrationID, id, kind, name, content string) (resource.Resource, error) {
	for i, r := range f.items[integrationID] {
		if r.ID == id {
			r.Kind, r.Name, r.Content = kind, name, content
			f.items[integrationID][i] = r
			return r, nil
		}
	}
	return resource.Resource{}, resource.ErrNotFound
}

func (f *fakeResources) Delete(_ context.Context, integrationID, id string) error {
	kept := make([]resource.Resource, 0, len(f.items[integrationID]))
	for _, r := range f.items[integrationID] {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	f.items[integrationID] = kept
	return nil
}

// fakeSnapshots is an in-memory stand-in for the snapshot service.
type fakeSnapshots struct {
	snaps     map[string]snapshot.Snapshot
	resources map[string][]snapshot.Resource
}

func (f *fakeSnapshots) Get(_ context.Context, id string) (snapshot.Snapshot, error) {
	s, ok := f.snaps[id]
	if !ok {
		return snapshot.Snapshot{}, snapshot.ErrNotFound
	}
	return s, nil
}

func (f *fakeSnapshots) ListResources(_ context.Context, snapshotID string) ([]snapshot.Resource, error) {
	return f.resources[snapshotID], nil
}

// newService wires the three fakes into a Service, returning all of them.
func newService() (*Service, *fakeIntegrations, *fakeResources, *fakeSnapshots) {
	ints, res := newFakeIntegrations(), newFakeResources()
	snaps := &fakeSnapshots{snaps: map[string]snapshot.Snapshot{}, resources: map[string][]snapshot.Resource{}}
	return NewService(ints, res, snaps), ints, res, snaps
}

// namesOf lists an integration's stored resource names, sorted.
func namesOf(t *testing.T, res *fakeResources, integrationID string) []string {
	t.Helper()
	items, _ := res.ListByIntegration(context.Background(), integrationID)
	out := make([]string, 0, len(items))
	for _, r := range items {
		out = append(out, r.Name)
	}
	sort.Strings(out)
	return out
}

func TestExportCarriesTheDefinitionAndEveryResource(t *testing.T) {
	svc, ints, res, _ := newService()
	ctx := context.Background()
	it, _ := ints.Create(ctx, "Order Sync", "name: order-sync\n", "")
	if _, err := res.Create(ctx, it.ID, "env", ".env.dev", "A=1\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	b, err := svc.Export(ctx, it.ID)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if b.Name != "Order Sync" || b.Definition != "name: order-sync\n" {
		t.Errorf("bundle = %+v", b)
	}
	if len(b.Resources) != 1 || b.Resources[0].Name != ".env.dev" || b.Resources[0].Content != "A=1\n" {
		t.Errorf("resources = %+v", b.Resources)
	}
}

// A tag's export is the frozen contents, not the working copy's — that is the
// whole point of exporting a version.
func TestExportSnapshotUsesTheFrozenContents(t *testing.T) {
	svc, ints, res, snaps := newService()
	ctx := context.Background()
	it, _ := ints.Create(ctx, "Order Sync", "definition: live\n", "")
	if _, err := res.Create(ctx, it.ID, "env", ".env.dev", "LIVE=1\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	snaps.snaps["s1"] = snapshot.Snapshot{ID: "s1", IntegrationID: it.ID, Tag: "v1", Definition: "definition: frozen\n"}
	snaps.resources["s1"] = []snapshot.Resource{{Kind: "env", Name: ".env.dev", Content: "FROZEN=1\n"}}

	b, err := svc.ExportSnapshot(ctx, "s1")
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	if b.Name != "Order Sync" || b.Tag != "v1" || b.Definition != "definition: frozen\n" {
		t.Errorf("bundle = %+v, want the frozen definition under the integration's name", b)
	}
	if len(b.Resources) != 1 || b.Resources[0].Content != "FROZEN=1\n" {
		t.Errorf("resources = %+v, want the frozen copy", b.Resources)
	}
}

func TestImportCreatesTheIntegrationAndItsResources(t *testing.T) {
	svc, _, res, _ := newService()
	ctx := context.Background()
	data, err := Write(Bundle{
		Name:       "Order Sync",
		Definition: "name: order-sync\n",
		Resources: []File{
			{Kind: kindEnv, Name: ".env.dev", Content: "A=1\n"},
			{Kind: kindTemplate, Name: "templates/a.tmpl", Content: "hi\n"},
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	created, err := svc.Import(ctx, data, "ignored.zip", "user-1")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if created.Name != "Order Sync" || created.Definition != "name: order-sync\n" {
		t.Errorf("created = %+v", created)
	}
	got := namesOf(t, res, created.ID)
	want := []string{".env.dev", "templates/a.tmpl"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("resources = %v, want %v", got, want)
	}
}

// The manifest's name wins when there is one; the caller's fallback names an
// archive that carries no manifest.
func TestImportNaming(t *testing.T) {
	svc, _, _, _ := newService()
	ctx := context.Background()

	manifestless := zipOf(t, map[string]string{"whatever.yaml": "a: b\n"})
	created, err := svc.Import(ctx, manifestless, "From A File", "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if created.Name != "From A File" {
		t.Errorf("name = %q, want the caller's fallback", created.Name)
	}

	created, err = svc.Import(ctx, manifestless, "", "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if created.Name != fallbackName {
		t.Errorf("name = %q, want %q", created.Name, fallbackName)
	}
}

// Importing the same bundle twice is a normal thing to do (that is what a copy
// is), so the second one is suffixed rather than refused.
func TestImportSuffixesANameAlreadyInUse(t *testing.T) {
	svc, _, _, _ := newService()
	ctx := context.Background()
	data, err := Write(Bundle{Name: "Order Sync", Definition: "a: b\n"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := svc.Import(ctx, data, "", ""); err != nil {
		t.Fatalf("first Import: %v", err)
	}
	second, err := svc.Import(ctx, data, "", "")
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if second.Name != "Order Sync (2)" {
		t.Errorf("name = %q, want %q", second.Name, "Order Sync (2)")
	}
}

// A resource that fails to store leaves nothing behind: a half-imported
// integration looks complete and is not.
func TestImportRollsBackWhenAResourceFails(t *testing.T) {
	svc, ints, res, _ := newService()
	ctx := context.Background()
	res.failCreate = errors.New("storage is down")
	data, err := Write(Bundle{
		Name:       "Order Sync",
		Definition: "a: b\n",
		Resources:  []File{{Kind: kindEnv, Name: ".env.dev", Content: "A=1\n"}},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := svc.Import(ctx, data, "", ""); err == nil {
		t.Fatal("Import succeeded, want the storage failure")
	}
	if len(ints.items) != 0 {
		t.Errorf("integrations = %+v, want the failed import rolled back", ints.items)
	}
}

func TestImportRejectsAnUnreadableArchive(t *testing.T) {
	svc, _, _, _ := newService()
	if _, err := svc.Import(context.Background(), []byte("not a zip"), "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Import err = %v, want ErrInvalid", err)
	}
}

// Replace keeps the integration's identity and reconciles the resource set:
// shared names keep their ids, new ones arrive, dropped ones go.
func TestReplaceReconcilesTheResourceSet(t *testing.T) {
	svc, ints, res, _ := newService()
	ctx := context.Background()
	it, _ := ints.Create(ctx, "Order Sync", "definition: old\n", "")
	kept, _ := res.Create(ctx, it.ID, "env", ".env.dev", "OLD=1\n")
	if _, err := res.Create(ctx, it.ID, "template", "templates/gone.tmpl", "bye\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	data, err := Write(Bundle{
		Name:       "A Different Name",
		Definition: "definition: new\n",
		Resources: []File{
			{Kind: kindEnv, Name: ".env.dev", Content: "NEW=1\n"},
			{Kind: kindTemplate, Name: "templates/added.tmpl", Content: "hello\n"},
		},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	updated, err := svc.Replace(ctx, it.ID, data, "user-1")
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if updated.ID != it.ID || updated.Name != "Order Sync" {
		t.Errorf("updated = %+v, want the same id and name (a replace is not a rename)", updated)
	}
	if updated.Definition != "definition: new\n" {
		t.Errorf("definition = %q, want the bundle's", updated.Definition)
	}
	got := namesOf(t, res, it.ID)
	want := []string{".env.dev", "templates/added.tmpl"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("resources = %v, want %v", got, want)
	}
	items, _ := res.ListByIntegration(ctx, it.ID)
	for _, r := range items {
		if r.Name == ".env.dev" {
			if r.ID != kept.ID {
				t.Errorf(".env.dev id = %q, want the existing %q kept", r.ID, kept.ID)
			}
			if r.Content != "NEW=1\n" {
				t.Errorf(".env.dev content = %q, want the bundle's", r.Content)
			}
		}
	}
}

func TestReplaceRejectsAnUnknownIntegration(t *testing.T) {
	svc, _, _, _ := newService()
	data, err := Write(Bundle{Name: "x", Definition: "a: b\n"})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := svc.Replace(context.Background(), "nope", data, ""); !errors.Is(err, integration.ErrNotFound) {
		t.Fatalf("Replace err = %v, want ErrNotFound", err)
	}
}

// A replace that fails part-way leaves the working copy as it was: the definition
// and the resources live in two modules with no transaction spanning them, so the
// previous state is read first and put back.
func TestReplaceRestoresThePreviousStateWhenReconcileFails(t *testing.T) {
	svc, ints, res, _ := newService()
	ctx := context.Background()
	it, _ := ints.Create(ctx, "Order Sync", "definition: old\n", "")
	if _, err := res.Create(ctx, it.ID, "env", ".env.dev", "OLD=1\n"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	data, err := Write(Bundle{
		Name:       "Order Sync",
		Definition: "definition: new\n",
		Resources:  []File{{Kind: kindTemplate, Name: "templates/added.tmpl", Content: "hello\n"}},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// The added resource is a create, which is what fails.
	res.failCreate = errors.New("storage is down")

	if _, err := svc.Replace(ctx, it.ID, data, ""); err == nil {
		t.Fatal("Replace succeeded, want the storage failure")
	}
	if got := ints.items[it.ID].Definition; got != "definition: old\n" {
		t.Errorf("definition = %q, want the previous one restored", got)
	}
	got := namesOf(t, res, it.ID)
	if len(got) != 1 || got[0] != ".env.dev" {
		t.Errorf("resources = %v, want the previous set restored", got)
	}
}

// The most likely reason a write failed is that the request was cancelled, so the
// undo runs on a context detached from it. Inheriting the cancellation would make
// the cleanup fail immediately and leave exactly the partial state it exists to
// remove.
func TestImportRollsBackEvenWhenTheRequestContextIsCancelled(t *testing.T) {
	svc, ints, res, _ := newService()
	ctx, cancel := context.WithCancel(context.Background())
	res.failCreate = context.Canceled
	data, err := Write(Bundle{
		Name:       "Order Sync",
		Definition: "a: b\n",
		Resources:  []File{{Kind: kindEnv, Name: ".env.dev", Content: "A=1\n"}},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The integration is created, then the resource fails and the caller goes away.
	go cancel()
	if _, err := svc.Import(ctx, data, "", ""); err == nil {
		t.Fatal("Import succeeded, want the failure")
	}
	cancel()
	if len(ints.items) != 0 {
		t.Errorf("integrations = %+v, want the failed import rolled back", ints.items)
	}
}
