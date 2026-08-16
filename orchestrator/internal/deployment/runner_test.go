package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// TestDeployCarriesTheRunnerToTheClusterAndTheRow covers both halves of the same
// mistake. The spec is what this deploy runs; the persisted row is what the NEXT
// rollout reads — and because the settings literal in Deploy is a field-by-field
// copy rather than a re-marshal of the request, a field left out of it reaches
// the cluster once and is then silently demoted on the first version bump.
func TestDeployCarriesTheRunnerToTheClusterAndTheRow(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Dr. Octo"}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Runner: "agentic"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Runner != kube.RunnerAgentic {
		t.Errorf("spec runner = %q, want %q", kc.gotSpec.Runner, kube.RunnerAgentic)
	}
	if got := ParseSettings(repo.gotSettings).Runner; got != "agentic" {
		t.Errorf("persisted runner = %q, want %q", got, "agentic")
	}
}

// TestDeployDefaultsToTheStandardRunner — a deployment that says nothing keeps
// the pod every integration has always had.
func TestDeployDefaultsToTheStandardRunner(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Runner != kube.RunnerStandard {
		t.Errorf("spec runner = %q, want %q", kc.gotSpec.Runner, kube.RunnerStandard)
	}
	if got := ParseSettings(repo.gotSettings).Runner; got != "" {
		t.Errorf("persisted runner = %q, want it left empty", got)
	}
}

// TestDeployRefusesAnUnconfiguredRunner — the alternative, quietly deploying the
// standard image, produces a healthy pod whose every command fails with "not
// found", which reads as a broken flow rather than a missing chart value.
func TestDeployRefusesAnUnconfiguredRunner(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Dr. Octo"}}
	kc := &fakeKube{runnersDisabled: map[kube.Runner]bool{kube.RunnerAgentic: true}}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1", Settings{Runner: "agentic"})
	if !errors.Is(err, ErrRunnerUnavailable) {
		t.Errorf("got %v, want ErrRunnerUnavailable", err)
	}
	if kc.applied {
		t.Error("kube.Apply should not run for a runner this installation cannot serve")
	}
}

// TestDeployRefusesAnUnknownRunner is a separate error from the one above because
// it is a separate mistake with a separate fix: a typo in the request, not a
// chart that is missing an image.
func TestDeployRefusesAnUnknownRunner(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1", Settings{Runner: "agentik"})
	if !errors.Is(err, ErrInvalidRunner) {
		t.Errorf("got %v, want ErrInvalidRunner", err)
	}
	if kc.applied {
		t.Error("kube.Apply should not run for an unknown runner")
	}
}

// TestRolloutPreservesTheRunner is the regression the whole feature turns on. A
// rollout takes a version, not a workload shape, so it reads the runner back from
// the stored row — and if it did not, the first version bump after an agentic
// deployment would replace its pods with distroless ones and every command it
// runs would start failing, with nothing linking that to the upgrade.
func TestRolloutPreservesTheRunner(t *testing.T) {
	repo := &fakeRepo{getRet: Deployment{
		ID: "dep-1", IntegrationID: "int-1",
		Settings: json.RawMessage(`{"replicas":1,"runner":"agentic"}`),
		Metadata: json.RawMessage(`{"slug":"dr-octo"}`),
	}}
	snaps := &fakeSnapshots{ret: snapshot.Snapshot{
		ID: "snap-2", IntegrationID: "int-1", Tag: "v2.0", Definition: exposableDef,
	}}
	kc := &fakeKube{status: kube.StatusRunning}
	svc := NewService(repo, &fakeIntegrations{}, kc, WithSnapshots(snaps))

	if _, err := svc.Rollout(context.Background(), "dep-1", "snap-2", nil, nil, nil); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	if kc.gotSpec.Runner != kube.RunnerAgentic {
		t.Errorf("spec runner = %q, want %q", kc.gotSpec.Runner, kube.RunnerAgentic)
	}
	if got := ParseSettings(repo.gotSettings).Runner; got != "agentic" {
		t.Errorf("persisted runner = %q, want it preserved", got)
	}
}

// TestRolloutRefusesARunnerTheInstallLost — better to refuse the rollout than to
// replace a working agentic pod with one that cannot run any of its commands.
func TestRolloutRefusesARunnerTheInstallLost(t *testing.T) {
	repo := &fakeRepo{getRet: Deployment{
		ID: "dep-1", IntegrationID: "int-1",
		Settings: json.RawMessage(`{"replicas":1,"runner":"agentic"}`),
		Metadata: json.RawMessage(`{"slug":"dr-octo"}`),
	}}
	snaps := &fakeSnapshots{ret: snapshot.Snapshot{
		ID: "snap-2", IntegrationID: "int-1", Tag: "v2.0", Definition: exposableDef,
	}}
	kc := &fakeKube{
		status:          kube.StatusRunning,
		runnersDisabled: map[kube.Runner]bool{kube.RunnerAgentic: true},
	}
	svc := NewService(repo, &fakeIntegrations{}, kc, WithSnapshots(snaps))

	_, err := svc.Rollout(context.Background(), "dep-1", "snap-2", nil, nil, nil)
	if !errors.Is(err, ErrRunnerUnavailable) {
		t.Errorf("got %v, want ErrRunnerUnavailable", err)
	}
}
