package deployment

import (
	"context"
	"log/slog"
	"time"
)

// Reconcile is the sweep that puts the database and the cluster back into
// agreement about what is deployed.
//
// It exists because nothing else ever asked. Deployment status is refreshed on
// read, and `refresh` falls back to the *cached* value whenever the cluster read
// fails or there is no cluster at all — so a row that said "running" when the
// cluster went away says "running" forever, and the deployments page lists
// workloads that have not existed since the cluster was rebuilt. The other
// direction was worse: nothing in the codebase ever listed the workloads this
// orchestrator manages, so a Deployment left behind by a failed rollback was
// invisible to the platform entirely and ran until somebody noticed the pod.
//
// What it does not do is guess. Every branch below is reached only from a cluster
// listing that succeeded, because "I could not see the cluster" and "the cluster
// is empty" are the same empty map, and one of them is an instruction to delete
// everything.

const (
	// reconcileGrace is how recently a row may have been touched and still be left
	// alone.
	//
	// A deploy writes its row before it calls the API server, so there is a window
	// in which a perfectly healthy deployment has no workload yet. Five minutes is
	// far longer than that window and far shorter than "the cluster went away",
	// which is the case this is for.
	reconcileGrace = 5 * time.Minute

	// reconcileInterval is how often the sweep runs. Drift is not urgent — nothing
	// is broken by a stale row for a few minutes, and the page that shows it is not
	// watched continuously — and the sweep reads the whole table, so this is set by
	// what is polite rather than by what is timely.
	reconcileInterval = 5 * time.Minute
)

// Reconciled counts what one sweep repaired.
type Reconciled struct {
	// RowsDeleted is deployments whose workload was gone.
	RowsDeleted int
	// WorkloadsDeleted is workloads whose row was gone.
	WorkloadsDeleted int
}

// Reconcile runs one sweep and reports what it repaired.
//
// The order is deliberate: rows first, then workloads. Deleting a row makes its
// workload an orphan, and doing rows first means this sweep collects it rather
// than the next one — a deployment whose cluster resources outlived its row by
// five minutes is exactly the state somebody would be looking at while wondering
// what happened.
func (s *Service) Reconcile(ctx context.Context) (Reconciled, error) {
	var out Reconciled
	if s.kube == nil {
		return out, nil
	}

	live, trusted, err := s.kube.DeploymentIDs(ctx)
	if err != nil {
		// Reported rather than repaired. A cluster that cannot be listed is a reason
		// to do nothing at all: every decision below is "the cluster does not have
		// this", and an error means the cluster was not asked.
		return out, err
	}
	if !trusted {
		slog.Debug("deployment reconcile: skipped, cluster view is not trustworthy")
		return out, nil
	}

	rows, err := s.repo.ListAll(ctx)
	if err != nil {
		return out, err
	}

	known := make(map[string]bool, len(rows))
	cutoff := time.Now().Add(-reconcileGrace)
	for _, row := range rows {
		known[row.ID] = true
		if live[row.ID] {
			continue
		}
		if row.LastUpdated.After(cutoff) {
			// Mid-deploy, most likely: the row is written before the API server is
			// called. Left for the next sweep, by which time it is one or the other.
			continue
		}

		// Asked again, by name, before anything is deleted. The listing above
		// answers from labels, and a label is metadata anyone with cluster access
		// can edit; the name is derived from this row's own id and cannot drift.
		// Deleting a row is irreversible and takes its settings and env bindings
		// with it, so it is worth one Get on a path this rare.
		exists, err := s.kube.DeploymentExists(ctx, row.ID)
		if err != nil {
			slog.Error("deployment reconcile: confirm workload is gone",
				"deploymentId", row.ID, "error", err)
			continue
		}
		if exists {
			slog.Warn("deployment reconcile: a workload exists but the listing missed it; "+
				"check its labels", "deploymentId", row.ID)
			continue
		}

		if s.removeOrphanedRow(ctx, row) {
			out.RowsDeleted++
		}
	}

	for id := range live {
		if known[id] {
			continue
		}
		if err := s.kube.Delete(ctx, id); err != nil {
			slog.Error("deployment reconcile: delete orphaned workload", "deploymentId", id, "error", err)
			continue
		}
		slog.Warn("deployment reconcile: deleted a workload with no deployment row",
			"deploymentId", id)
		out.WorkloadsDeleted++
	}

	return out, nil
}

// removeOrphanedRow deletes a deployment whose workload is gone, along with the
// resources Undeploy would have cleaned up.
//
// It repeats Undeploy's cleanup rather than calling it, and the difference is the
// one line that matters: Undeploy deletes the workload first and aborts if that
// fails, which is right when a user pressed Remove and wrong here — the workload
// is already gone, and its absence is the whole reason this is running.
//
// Everything after the row is best-effort and logged, exactly as Undeploy treats
// it. The row is what the UI reads; a stranded internal Service or a few KV rows
// are invisible and cost nothing but space.
func (s *Service) removeOrphanedRow(ctx context.Context, row Deployment) bool {
	if err := s.repo.Delete(ctx, row.ID); err != nil {
		slog.Error("deployment reconcile: delete orphaned row", "deploymentId", row.ID, "error", err)
		return false
	}
	slog.Warn("deployment reconcile: deleted a deployment row with no workload",
		"deploymentId", row.ID, "integrationId", row.IntegrationID,
		"status", row.Status, "lastUpdated", row.LastUpdated)

	if slug := ParseMetadata(row.Metadata).Slug; slug != "" {
		if err := s.kube.DeleteInternalService(ctx, slug); err != nil {
			slog.Error("deployment reconcile: delete internal service",
				"deploymentId", row.ID, "slug", slug, "error", err)
		}
	}
	for _, cleaner := range s.cleaners {
		if err := cleaner.DeleteByDeployment(ctx, row.ID); err != nil {
			slog.Error("deployment reconcile: clean deployment store",
				"deploymentId", row.ID, "error", err)
		}
	}
	return true
}

// ReconcileInterval is how often a caller should run the sweep. Exported so the
// wiring does not have to restate a number this package chose.
func ReconcileInterval() time.Duration { return reconcileInterval }
