package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// stale is a row old enough that the grace period does not protect it.
func stale(id, integrationID string) Deployment {
	return Deployment{
		ID:            id,
		IntegrationID: integrationID,
		Status:        "running",
		LastUpdated:   time.Now().Add(-time.Hour),
		Metadata:      json.RawMessage(`{"slug":"` + id + `"}`),
	}
}

// reconciler wires a service with a repo and a cluster view, and a trusted
// listing — the untrusted cases say so explicitly.
func reconciler(rows []Deployment, live map[string]bool) (*Service, *fakeRepo, *fakeKube) {
	repo := &fakeRepo{allRet: rows}
	k := &fakeKube{liveIDs: live, liveTrusted: true}
	return NewService(repo, &fakeIntegrations{}, k), repo, k
}

// The case the whole file exists for: the cluster was rebuilt, so every row
// describes a workload that no longer exists and the deployments page lists them
// all as running.
func TestReconcileDeletesRowsWhoseWorkloadIsGone(t *testing.T) {
	svc, repo, _ := reconciler(
		[]Deployment{stale("dep-1", "int-1"), stale("dep-2", "int-1")},
		map[string]bool{},
	)

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got.RowsDeleted != 2 {
		t.Errorf("rowsDeleted = %d, want 2", got.RowsDeleted)
	}
	if len(repo.deletedIDs) != 2 {
		t.Errorf("deleted %v, want both rows", repo.deletedIDs)
	}
}

// The other direction, which nothing in the platform could even see before: a
// failed rollback leaves a Deployment running with no row to describe it.
func TestReconcileDeletesWorkloadsWithNoRow(t *testing.T) {
	svc, _, k := reconciler(nil, map[string]bool{"dep-orphan": true})

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got.WorkloadsDeleted != 1 {
		t.Errorf("workloadsDeleted = %d, want 1", got.WorkloadsDeleted)
	}
	if len(k.deletedIDs) != 1 || k.deletedIDs[0] != "dep-orphan" {
		t.Errorf("deleted %v, want [dep-orphan]", k.deletedIDs)
	}
}

func TestReconcileLeavesMatchedDeploymentsAlone(t *testing.T) {
	svc, repo, k := reconciler(
		[]Deployment{stale("dep-1", "int-1")},
		map[string]bool{"dep-1": true},
	)

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got != (Reconciled{}) {
		t.Errorf("repaired %+v, want nothing", got)
	}
	if repo.deleted || k.deleted {
		t.Error("want nothing deleted when the two sides agree")
	}
}

// A deploy writes its row before it calls the API server, so there is a window in
// which a healthy deployment has no workload yet. Reaping it would delete a
// deployment somebody is in the middle of making.
func TestReconcileSpareARowInsideTheGracePeriod(t *testing.T) {
	fresh := stale("dep-new", "int-1")
	fresh.LastUpdated = time.Now()

	svc, repo, _ := reconciler([]Deployment{fresh}, map[string]bool{})

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got.RowsDeleted != 0 || repo.deleted {
		t.Error("want a row inside the grace period left alone")
	}
}

// The failure mode this whole design is arranged against: an unreadable cluster
// produces the same empty map an empty cluster does, and acting on it would
// delete every deployment in the installation.
func TestReconcileDeletesNothingWhenTheClusterCannotBeListed(t *testing.T) {
	repo := &fakeRepo{allRet: []Deployment{stale("dep-1", "int-1"), stale("dep-2", "int-1")}}
	k := &fakeKube{liveErr: errors.New("connection refused")}
	svc := NewService(repo, &fakeIntegrations{}, k)

	got, err := svc.Reconcile(context.Background())

	if err == nil {
		t.Error("want the listing failure reported")
	}
	if got.RowsDeleted != 0 || repo.deleted {
		t.Error("want nothing deleted from a cluster that was never successfully asked")
	}
}

// The same hazard through the other door: the informer cache has not loaded yet,
// so it is empty and correct about nothing.
func TestReconcileDeletesNothingWhenTheClusterViewIsUntrusted(t *testing.T) {
	repo := &fakeRepo{allRet: []Deployment{stale("dep-1", "int-1")}}
	k := &fakeKube{liveIDs: map[string]bool{}, liveTrusted: false}
	svc := NewService(repo, &fakeIntegrations{}, k)

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got.RowsDeleted != 0 || repo.deleted {
		t.Error("want nothing deleted from an unsynced cache")
	}
}

// Running without cluster access is a supported way to run, and it is exactly the
// configuration in which every row looks orphaned.
func TestReconcileIsANoOpWithoutACluster(t *testing.T) {
	repo := &fakeRepo{allRet: []Deployment{stale("dep-1", "int-1")}}
	svc := NewService(repo, &fakeIntegrations{}, nil)

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got != (Reconciled{}) || repo.deleted {
		t.Error("want no cluster access to mean no opinion")
	}
}

// Deleting the row is the point; the rest is what Undeploy also does, on the same
// best-effort terms.
func TestReconcileCleansUpAfterADeletedRow(t *testing.T) {
	svc, _, k := reconciler([]Deployment{stale("dep-1", "int-1")}, map[string]bool{})
	cleaner := &fakeCleaner{}
	svc.cleaners = append(svc.cleaners, cleaner)

	if _, err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if !k.internalDeleted || k.gotInternalSlug != "dep-1" {
		t.Errorf("internal service: deleted=%v slug=%q, want it removed", k.internalDeleted, k.gotInternalSlug)
	}
	if cleaner.cleaned != "dep-1" {
		t.Errorf("cleaned %q, want the deployment's stores dropped", cleaner.cleaned)
	}
}

// A row that could not be deleted is not counted as repaired, and the sweep keeps
// going: one bad row must not strand every row after it.
func TestReconcileKeepsGoingPastAFailedDelete(t *testing.T) {
	repo := &fakeRepo{
		allRet:    []Deployment{stale("dep-1", "int-1")},
		deleteErr: errors.New("deadlock detected"),
	}
	k := &fakeKube{liveIDs: map[string]bool{"dep-orphan": true}, liveTrusted: true}
	svc := NewService(repo, &fakeIntegrations{}, k)

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got.RowsDeleted != 0 {
		t.Errorf("rowsDeleted = %d, want the failed one uncounted", got.RowsDeleted)
	}
	if got.WorkloadsDeleted != 1 {
		t.Errorf("workloadsDeleted = %d, want the sweep to have continued", got.WorkloadsDeleted)
	}
}

// fakeCleaner stands in for the KV store's deployment-scoped cleanup.
type fakeCleaner struct{ cleaned string }

func (f *fakeCleaner) DeleteByDeployment(_ context.Context, id string) error {
	f.cleaned = id
	return nil
}

// The listing answers from labels, which anyone with cluster access can edit.
// Deleting a row is irreversible and takes its settings and env bindings with it,
// so the sweep asks again by name — the identity derived from the row's own id,
// which cannot drift — before removing anything.
//
// Without the second question a stripped label deletes the row of a running
// deployment, and the workload can never be collected either: it no longer
// matches the selector that would have found it.
func TestReconcileConfirmsAWorkloadIsGoneBeforeDeletingItsRow(t *testing.T) {
	svc, repo, k := reconciler([]Deployment{stale("dep-1", "int-1")}, map[string]bool{})
	// The label listing missed it; the object is really there.
	k.existsOverride = map[string]bool{"dep-1": true}

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got.RowsDeleted != 0 || repo.deleted {
		t.Error("want the row kept: the workload exists under its own name")
	}
}

// A confirmation that could not be made is not a confirmation. Failing it must
// leave the row alone rather than fall through to the delete.
func TestReconcileKeepsARowItCouldNotConfirm(t *testing.T) {
	svc, repo, k := reconciler([]Deployment{stale("dep-1", "int-1")}, map[string]bool{})
	k.existsErr = errors.New("connection reset")

	got, err := svc.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if got.RowsDeleted != 0 || repo.deleted {
		t.Error("want the row kept when its absence could not be confirmed")
	}
}
