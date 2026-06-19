package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/juancavallotti/eip-go/orchestrator/internal/integration"
	"github.com/juancavallotti/eip-go/orchestrator/internal/kube"
)

// fakeRepo is an in-memory deployment repository stand-in.
type fakeRepo struct {
	created     Deployment
	createErr   error
	gotMetadata json.RawMessage
	getRet      Deployment
	getErr      error
	statusCalls int
	deleted     bool
	deleteErr   error
}

func (f *fakeRepo) Create(_ context.Context, integrationID, status string, md json.RawMessage) (Deployment, error) {
	f.gotMetadata = md
	if f.createErr != nil {
		return Deployment{}, f.createErr
	}
	d := f.created
	d.IntegrationID = integrationID
	d.Status = status
	return d, nil
}

func (f *fakeRepo) Get(_ context.Context, _ string) (Deployment, error) {
	return f.getRet, f.getErr
}

func (f *fakeRepo) ListByIntegration(_ context.Context, _ string) ([]Deployment, error) {
	return []Deployment{f.getRet}, f.getErr
}

func (f *fakeRepo) UpdateStatus(_ context.Context, _, _ string) error {
	f.statusCalls++
	return nil
}

func (f *fakeRepo) Delete(_ context.Context, _ string) error {
	f.deleted = true
	return f.deleteErr
}

// fakeIntegrations returns a preset integration or error.
type fakeIntegrations struct {
	ret integration.Integration
	err error
}

func (f *fakeIntegrations) Get(_ context.Context, _ string) (integration.Integration, error) {
	return f.ret, f.err
}

// fakeKube records calls and returns preset results.
type fakeKube struct {
	applied    bool
	applyErr   error
	gotSpec    kube.Spec
	status     string
	statusErr  error
	deleted    bool
	deleteErr  error
}

func (f *fakeKube) Apply(_ context.Context, spec kube.Spec) error {
	f.applied = true
	f.gotSpec = spec
	return f.applyErr
}

func (f *fakeKube) Status(_ context.Context, _ string) (string, error) {
	return f.status, f.statusErr
}

func (f *fakeKube) Delete(_ context.Context, _ string) error {
	f.deleted = true
	return f.deleteErr
}

func TestDeployHappyPath(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: "service:\n  name: orders\n"}}
	kc := &fakeKube{status: kube.StatusRunning}
	svc := NewService(repo, integrations, kc)

	d, err := svc.Deploy(context.Background(), "int-1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !kc.applied {
		t.Error("expected kube.Apply to be called")
	}
	if kc.gotSpec.ID != "dep-1" || kc.gotSpec.Definition == "" {
		t.Errorf("spec not threaded through: %+v", kc.gotSpec)
	}
	if d.Status != kube.StatusRunning {
		t.Errorf("status = %q, want refreshed to running", d.Status)
	}
	if MetadataName(repo.gotMetadata) != "Orders" {
		t.Errorf("metadata name = %q, want Orders", MetadataName(repo.gotMetadata))
	}
}

func TestDeployMissingIntegration(t *testing.T) {
	repo := &fakeRepo{}
	integrations := &fakeIntegrations{err: integration.ErrNotFound}
	kc := &fakeKube{}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "missing")
	if !errors.Is(err, ErrIntegrationNotFound) {
		t.Errorf("got %v, want ErrIntegrationNotFound", err)
	}
	if kc.applied {
		t.Error("kube.Apply should not run when the integration is missing")
	}
}

func TestDeployRollsBackOnKubeFailure(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{applyErr: errors.New("boom")}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !kc.deleted {
		t.Error("expected kube.Delete during rollback")
	}
	if !repo.deleted {
		t.Error("expected repo.Delete during rollback")
	}
}

func TestDeployUnavailableWithoutKube(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeIntegrations{}, nil)
	if _, err := svc.Deploy(context.Background(), "int-1"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("got %v, want ErrUnavailable", err)
	}
}

func TestUndeployDeletesResourcesAndRow(t *testing.T) {
	repo := &fakeRepo{getRet: Deployment{ID: "dep-1"}}
	kc := &fakeKube{}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if err := svc.Undeploy(context.Background(), "dep-1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !kc.deleted || !repo.deleted {
		t.Errorf("expected both kube and repo delete (kube=%v repo=%v)", kc.deleted, repo.deleted)
	}
}

func TestUndeployMissing(t *testing.T) {
	repo := &fakeRepo{getErr: ErrNotFound}
	kc := &fakeKube{}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if err := svc.Undeploy(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
	if kc.deleted {
		t.Error("kube.Delete should not run for an unknown deployment")
	}
}
