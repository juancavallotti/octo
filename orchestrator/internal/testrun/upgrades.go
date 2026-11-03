package testrun

import (
	"context"
	"slices"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
	"github.com/juancavallotti/octo/orchestrator/internal/runtimeversion"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// Upgrade is one deployment as an upgrade sees it: the runtime it is on, the one
// a rollout would move it to, whether it has suites to prove that move, and how
// they last went.
type Upgrade struct {
	Deployment     deployment.Deployment
	Name           string
	Tag            string
	SnapshotID     string
	RuntimeVersion string
	// UpgradeTo is the newer release a rollout would move it onto, "" when it is
	// current.
	UpgradeTo string
	HasSuites bool
	// LatestRun is the deployment's most recent test run, nil when it has none.
	LatestRun *Run
	// LatestRunStale says the deployment was deployed or rolled out after that
	// run, so its version, env or tracing may no longer be what the run tested.
	LatestRunStale bool
}

// Upgrades is every deployment on the installation as an upgrade sees it, and
// the runtime release the install deploys.
func (s *Service) Upgrades(ctx context.Context) (string, []Upgrade, error) {
	current := s.deployments.CurrentRuntime()
	deps, err := s.deployments.List(ctx)
	if err != nil {
		return "", nil, err
	}
	latest, err := s.Latest(ctx)
	if err != nil {
		return "", nil, err
	}
	ret := make([]Upgrade, 0, len(deps))
	for _, d := range deps {
		meta := deployment.ParseMetadata(d.Metadata)
		settings := deployment.ParseSettings(d.Settings)
		_, version := d.Runtime()
		u := Upgrade{
			Deployment:     d,
			Name:           meta.Name,
			Tag:            meta.Tag,
			SnapshotID:     settings.SnapshotID,
			RuntimeVersion: version,
		}
		if runtimeversion.Behind(version, current) {
			u.UpgradeTo = current
		}
		if u.HasSuites, err = s.hasSuites(ctx, settings.SnapshotID); err != nil {
			return "", nil, err
		}
		if run, ok := latest[d.ID]; ok {
			u.LatestRun = &run
			u.LatestRunStale = isStale(run, settings.SnapshotID, meta.RolledOutAt)
		}
		ret = append(ret, u)
	}
	return current, ret, nil
}

// hasSuites reports whether a snapshot froze any test suites. A deployment
// running no snapshot has none to run.
func (s *Service) hasSuites(ctx context.Context, snapshotID string) (bool, error) {
	if snapshotID == "" || s.snapshots == nil {
		return false, nil
	}
	resources, err := s.snapshots.ListResources(ctx, snapshotID)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(resources, func(r snapshot.Resource) bool {
		return projectfile.IsSuite(r.Name)
	}), nil
}

// isStale reports whether run no longer speaks for a deployment running
// snapshotID, last rolled out at rolledOutAt: it tested another snapshot, or the
// deployment was rolled out after it — by anything but the run's own rollout.
func isStale(run Run, snapshotID string, rolledOutAt *time.Time) bool {
	if run.SnapshotID != snapshotID {
		return true
	}
	if rolledOutAt == nil {
		return false
	}
	since := run.CreatedAt
	if run.RolledOutAt != nil {
		since = *run.RolledOutAt
	}
	return rolledOutAt.After(since)
}
