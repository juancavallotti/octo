package secret

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRepo is an in-memory catalog. It records the set/delete calls so tests can
// assert the value never reaches it (it stores only names).
type fakeRepo struct {
	names    map[string]Secret
	upserted []string
	deleted  []string
	listErr  error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{names: map[string]Secret{}} }

func (f *fakeRepo) Upsert(_ context.Context, name string) (Secret, error) {
	f.upserted = append(f.upserted, name)
	s := Secret{Name: name, CreatedAt: time.Unix(1, 0), LastUpdated: time.Unix(2, 0)}
	f.names[name] = s
	return s, nil
}

func (f *fakeRepo) List(_ context.Context) ([]Secret, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]Secret, 0, len(f.names))
	for _, s := range f.names {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeRepo) Delete(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	if _, ok := f.names[name]; !ok {
		return ErrNotFound
	}
	delete(f.names, name)
	return nil
}

// fakeKube records the values written so a test can confirm a set happened, and
// the keys deleted.
type fakeKube struct {
	values  map[string]string
	deleted []string
	setErr  error
	// listErr is how a cluster that could not be asked is spelled, which is the
	// case the reconciler must refuse to act on.
	listErr error
}

func newFakeKube() *fakeKube { return &fakeKube{values: map[string]string{}} }

func (f *fakeKube) SetSecret(_ context.Context, name, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.values[name] = value
	return nil
}

// ListSecretNames answers from the same map SetSecret writes to, so a test does
// not have to keep two views of the cluster in step.
func (f *fakeKube) ListSecretNames(_ context.Context) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]string, 0, len(f.values))
	for name := range f.values {
		out = append(out, name)
	}
	return out, nil
}

func (f *fakeKube) DeleteSecretKey(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	delete(f.values, name)
	return nil
}

// fakeRefs answers the in-use check.
type fakeRefs struct {
	used map[string]bool
	err  error
}

func (f *fakeRefs) SecretReferenced(_ context.Context, name string) (bool, error) {
	return f.used[name], f.err
}

func newService(repo *fakeRepo, kube *fakeKube, refs *fakeRefs) *Service {
	return NewService(repo, kube, refs)
}

func TestCreateStoresValueInKubeAndNameInCatalog(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	svc := newService(repo, kube, &fakeRefs{})

	s, err := svc.Create(context.Background(), "API_KEY", "supersecret")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.Name != "API_KEY" {
		t.Errorf("name = %q, want API_KEY", s.Name)
	}
	if kube.values["API_KEY"] != "supersecret" {
		t.Errorf("value not written to kube: %v", kube.values)
	}
	if len(repo.upserted) != 1 || repo.upserted[0] != "API_KEY" {
		t.Errorf("catalog upsert = %v, want [API_KEY]", repo.upserted)
	}
}

func TestCreateRejectsInvalidNameBeforeWriting(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	svc := newService(repo, kube, &fakeRefs{})

	if _, err := svc.Create(context.Background(), "lower-case", "v"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("err = %v, want ErrInvalidName", err)
	}
	if len(kube.values) != 0 || len(repo.upserted) != 0 {
		t.Error("nothing should be written when the name is invalid")
	}
}

func TestCreateUnavailableWhenNoKube(t *testing.T) {
	svc := NewService(newFakeRepo(), nil, &fakeRefs{})
	if _, err := svc.Create(context.Background(), "API_KEY", "v"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestListReturnsNamesOnly(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	svc := newService(repo, kube, &fakeRefs{})
	_, _ = svc.Create(context.Background(), "API_KEY", "v")

	items, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Name != "API_KEY" {
		t.Fatalf("items = %+v, want one API_KEY", items)
	}
	// The Secret type has no value field — listing cannot leak a value by design.
}

func TestDeleteHappyPath(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	svc := newService(repo, kube, &fakeRefs{})
	_, _ = svc.Create(context.Background(), "API_KEY", "v")

	if err := svc.Delete(context.Background(), "API_KEY", false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := kube.values["API_KEY"]; ok {
		t.Error("value should be removed from kube")
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "API_KEY" {
		t.Errorf("catalog delete = %v, want [API_KEY]", repo.deleted)
	}
}

func TestDeleteBlockedWhenInUse(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	refs := &fakeRefs{used: map[string]bool{"API_KEY": true}}
	svc := newService(repo, kube, refs)
	_, _ = svc.Create(context.Background(), "API_KEY", "v")

	if err := svc.Delete(context.Background(), "API_KEY", false); !errors.Is(err, ErrInUse) {
		t.Fatalf("err = %v, want ErrInUse", err)
	}
	// Nothing deleted while in use.
	if len(kube.deleted) != 0 || len(repo.deleted) != 0 {
		t.Error("an in-use secret must not be deleted")
	}

	// force overrides the in-use guard.
	if err := svc.Delete(context.Background(), "API_KEY", true); err != nil {
		t.Fatalf("forced Delete: %v", err)
	}
	if len(kube.deleted) != 1 {
		t.Error("forced delete should remove the value")
	}
}

func TestValidName(t *testing.T) {
	accept := []string{"FOO", "FOO_BAR", "A1", "_X", "API_KEY"}
	reject := []string{"", "foo", "1FOO", "FOO-BAR", "FOO.BAR", "FOO BAR", "fooBar"}
	for _, n := range accept {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) = false, want true", n)
		}
	}
	for _, n := range reject {
		if ValidName(n) {
			t.Errorf("ValidName(%q) = true, want false", n)
		}
	}
}

// The catalogue and the values are two writes to two systems, so a cluster
// rebuilt from scratch leaves the platform listing secrets that are gone — and a
// deployment binding one fails to start while the secrets page insists it is
// there.
func TestReconcileDropsCatalogueEntriesTheClusterDoesNotHave(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	svc := NewService(repo, kube, &fakeRefs{})
	ctx := context.Background()

	if _, err := svc.Create(ctx, "KEPT", "v"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A catalogue row with no key behind it — what a rebuilt cluster leaves.
	repo.names["GONE"] = Secret{Name: "GONE"}

	dropped, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
	if _, still := repo.names["GONE"]; still {
		t.Error("want the entry with no key removed")
	}
	if _, gone := repo.names["KEPT"]; !gone {
		t.Error("want the entry the cluster still has left alone")
	}
}

// The asymmetry is the point: deleting key material because a database row is
// missing is not a trade worth making unattended, and a catalogue restored from
// an older backup is exactly when the key is the thing worth keeping.
func TestReconcileNeverDeletesAKeyTheCatalogueDoesNotList(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	svc := NewService(repo, kube, &fakeRefs{})
	kube.values["ORPHANED_KEY"] = "value"

	if _, err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if _, still := kube.values["ORPHANED_KEY"]; !still {
		t.Error("want a key with no catalogue row kept")
	}
	if len(kube.deleted) != 0 {
		t.Errorf("deleted %v from the cluster, want nothing", kube.deleted)
	}
}

// An empty answer from a cluster that was never successfully asked is an
// instruction to delete the whole catalogue.
func TestReconcileDeletesNothingWhenTheClusterCannotBeListed(t *testing.T) {
	repo, kube := newFakeRepo(), newFakeKube()
	kube.listErr = errors.New("connection refused")
	repo.names["SOMETHING"] = Secret{Name: "SOMETHING"}
	svc := NewService(repo, kube, &fakeRefs{})

	dropped, err := svc.Reconcile(context.Background())

	if err == nil {
		t.Error("want the listing failure reported")
	}
	if dropped != 0 || len(repo.deleted) != 0 {
		t.Errorf("dropped %d / %v, want nothing", dropped, repo.deleted)
	}
}

// Without cluster access there is no cluster to disagree with.
func TestReconcileIsANoOpWithoutACluster(t *testing.T) {
	repo := newFakeRepo()
	repo.names["SOMETHING"] = Secret{Name: "SOMETHING"}
	svc := NewService(repo, nil, &fakeRefs{})

	dropped, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if dropped != 0 || len(repo.deleted) != 0 {
		t.Error("want no cluster access to mean no opinion")
	}
}
