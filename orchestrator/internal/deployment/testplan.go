package deployment

import (
	"context"

	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// TestPlan is what running a deployment's suites against one of its
// integration's snapshots needs: the frozen definition and files, and the env
// the deployment's pods would be given for it.
type TestPlan struct {
	DeploymentID  string
	IntegrationID string
	SnapshotID    string
	Tag           string
	Definition    string
	// Resources are every file frozen under the snapshot that is not config,
	// suites included.
	Resources []snapshot.Resource
	Env       map[string]string
	SecretEnv map[string]string
	// RuntimePin is the release the deployment is pinned to, "" when it follows
	// the install.
	RuntimePin string
}

// TestPlan resolves what testing deployment id against snapshotID needs. An
// empty snapshotID means the snapshot the deployment runs now.
//
// The env is the deployment's stored bindings resolved exactly as a rollout of
// that snapshot would resolve them, so a suite sees what the pods would see.
func (s *Service) TestPlan(ctx context.Context, id, snapshotID string) (TestPlan, error) {
	if s.kube == nil {
		return TestPlan{}, ErrUnavailable
	}
	if s.snapshots == nil {
		return TestPlan{}, ErrSnapshotRequired
	}
	dep, err := s.repo.Get(ctx, id)
	if err != nil {
		return TestPlan{}, err
	}
	settings := ParseSettings(dep.Settings)
	if snapshotID == "" {
		snapshotID = settings.SnapshotID
	}
	snap, err := s.resolveSnapshot(ctx, dep.IntegrationID, snapshotID)
	if err != nil {
		return TestPlan{}, err
	}
	_, runtimeEnv, _ := resolveRuntimeEnv(snap.Definition)
	literal, secret, err := s.resolveEnvBindings(ctx, runtimeEnv, settings.Env)
	if err != nil {
		return TestPlan{}, err
	}
	resources, err := s.snapshots.ListResources(ctx, snap.ID)
	if err != nil {
		return TestPlan{}, err
	}
	return TestPlan{
		DeploymentID:  dep.ID,
		IntegrationID: dep.IntegrationID,
		SnapshotID:    snap.ID,
		Tag:           snap.Tag,
		Definition:    snap.Definition,
		Resources:     resources,
		Env:           literal,
		SecretEnv:     secret,
		RuntimePin:    settings.RuntimeVersion,
	}, nil
}
