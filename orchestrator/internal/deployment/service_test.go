package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/juancavallotti/eip-go/orchestrator/internal/integration"
	"github.com/juancavallotti/eip-go/orchestrator/internal/kube"
)

// fakeRepo is an in-memory deployment repository stand-in.
type fakeRepo struct {
	created     Deployment
	createErr   error
	gotSettings json.RawMessage
	gotMetadata json.RawMessage
	getRet      Deployment
	getErr      error
	listRet     []Deployment
	listErr     error
	statusCalls       int
	updateSettingsErr error
	deleted           bool
	deleteErr         error
	// slugOwner/subdomainOwner stand in for an existing deployment claiming a
	// slug/subdomain; empty means "not found".
	slugOwner      string
	subdomainOwner string
}

func (f *fakeRepo) Create(_ context.Context, integrationID, status string, settings, md json.RawMessage) (Deployment, error) {
	f.gotSettings = settings
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
	return f.listRet, f.listErr
}

func (f *fakeRepo) IntegrationIDBySlug(_ context.Context, _ string) (string, bool, error) {
	return f.slugOwner, f.slugOwner != "", nil
}

func (f *fakeRepo) IntegrationIDBySubdomain(_ context.Context, _ string) (string, bool, error) {
	return f.subdomainOwner, f.subdomainOwner != "", nil
}

func (f *fakeRepo) UpdateStatus(_ context.Context, _, _ string) error {
	f.statusCalls++
	return nil
}

func (f *fakeRepo) UpdateSettings(_ context.Context, _ string, settings json.RawMessage) error {
	f.gotSettings = settings
	return f.updateSettingsErr
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
	applied         bool
	applyErr        error
	gotSpec         kube.Spec
	status          string
	statusErr       error
	scaled          bool
	gotReplicas     int32
	scaleErr        error
	deleted         bool
	deleteErr       error
	internalDeleted bool
	gotInternalSlug string
	externalEnabled bool
}

func (f *fakeKube) Apply(_ context.Context, spec kube.Spec) error {
	f.applied = true
	f.gotSpec = spec
	return f.applyErr
}

func (f *fakeKube) Status(_ context.Context, _ string) (kube.Status, error) {
	return kube.Status{Phase: f.status}, f.statusErr
}

func (f *fakeKube) Scale(_ context.Context, _ string, replicas int32) error {
	f.scaled = true
	f.gotReplicas = replicas
	return f.scaleErr
}

func (f *fakeKube) Delete(_ context.Context, _ string) error {
	f.deleted = true
	return f.deleteErr
}

func (f *fakeKube) InternalURL(slug string, port int) string {
	if slug == "" {
		return ""
	}
	if port < 1 {
		port = 8080
	}
	return fmt.Sprintf("http://octo-int-%s.octo-dev:%d", slug, port)
}

func (f *fakeKube) DeleteInternalService(_ context.Context, slug string) error {
	f.internalDeleted = true
	f.gotInternalSlug = slug
	return nil
}

func (f *fakeKube) ExternalEnabled() bool { return f.externalEnabled }

func (f *fakeKube) ExternalURL(subdomain string) string {
	if !f.externalEnabled || subdomain == "" {
		return ""
	}
	return "https://" + subdomain + ".octo.example.com"
}

func TestScaleUpdatesClusterAndSettings(t *testing.T) {
	repo := &fakeRepo{
		getRet: Deployment{ID: "dep-1", Settings: json.RawMessage(`{"replicas":1,"expose":"external","subdomain":"orders"}`)},
	}
	kc := &fakeKube{status: kube.StatusRunning}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	d, err := svc.Scale(context.Background(), "dep-1", 4)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !kc.scaled || kc.gotReplicas != 4 {
		t.Errorf("kube.Scale not called with 4: scaled=%v replicas=%d", kc.scaled, kc.gotReplicas)
	}
	// The new count is persisted while expose/subdomain are preserved.
	got := ParseSettings(repo.gotSettings)
	if got.Replicas != 4 || got.Expose != ExposeExternal || got.Subdomain != "orders" {
		t.Errorf("persisted settings = %+v, want replicas=4 and external/orders preserved", got)
	}
	if d.Status != kube.StatusRunning {
		t.Errorf("status = %q, want refreshed to running", d.Status)
	}
}

func TestScaleNormalizesBelowOne(t *testing.T) {
	repo := &fakeRepo{getRet: Deployment{ID: "dep-1", Settings: json.RawMessage(`{"replicas":3}`)}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if _, err := svc.Scale(context.Background(), "dep-1", 0); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotReplicas != 1 {
		t.Errorf("replicas = %d, want normalized to 1", kc.gotReplicas)
	}
}

func TestScaleUnknownDeployment(t *testing.T) {
	repo := &fakeRepo{getErr: ErrNotFound}
	kc := &fakeKube{}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if _, err := svc.Scale(context.Background(), "missing", 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if kc.scaled {
		t.Error("kube.Scale should not be called for an unknown deployment")
	}
}

func TestDeployHappyPath(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: "service:\n  name: orders\n"}}
	kc := &fakeKube{status: kube.StatusRunning}
	svc := NewService(repo, integrations, kc)

	d, err := svc.Deploy(context.Background(), "int-1", Settings{})
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

func TestDeployThreadsReplicasAndSlug(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "My Orders API!"}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	d, err := svc.Deploy(context.Background(), "int-1", Settings{Replicas: 3})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Replicas != 3 {
		t.Errorf("spec replicas = %d, want 3", kc.gotSpec.Replicas)
	}
	if kc.gotSpec.Slug != "my-orders-api" {
		t.Errorf("spec slug = %q, want my-orders-api", kc.gotSpec.Slug)
	}
	if got := ParseSettings(repo.gotSettings).Replicas; got != 3 {
		t.Errorf("persisted replicas = %d, want 3", got)
	}
	meta := ParseMetadata(repo.gotMetadata)
	if meta.Slug != "my-orders-api" || meta.InternalURL == "" {
		t.Errorf("metadata slug/url not set: %+v", meta)
	}
	_ = d
}

func TestDeployDefaultsReplicasToOne(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Replicas != 1 {
		t.Errorf("spec replicas = %d, want 1 (default)", kc.gotSpec.Replicas)
	}
}

func TestUndeployRemovesInternalServiceWhenLast(t *testing.T) {
	repo := &fakeRepo{
		getRet:  Deployment{ID: "dep-1", IntegrationID: "int-1", Metadata: []byte(`{"slug":"orders"}`)},
		listRet: nil, // no deployments remain after delete
	}
	kc := &fakeKube{}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if err := svc.Undeploy(context.Background(), "dep-1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !kc.internalDeleted || kc.gotInternalSlug != "orders" {
		t.Errorf("expected internal service delete for slug orders (deleted=%v slug=%q)", kc.internalDeleted, kc.gotInternalSlug)
	}
}

func TestUndeployKeepsInternalServiceWhenOthersRemain(t *testing.T) {
	repo := &fakeRepo{
		getRet:  Deployment{ID: "dep-1", IntegrationID: "int-1", Metadata: []byte(`{"slug":"orders"}`)},
		listRet: []Deployment{{ID: "dep-2", IntegrationID: "int-1"}},
	}
	kc := &fakeKube{}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if err := svc.Undeploy(context.Background(), "dep-1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.internalDeleted {
		t.Error("internal service should be kept while other deployments of the integration remain")
	}
}

// exposableDef declares HTTP_PORT, which is what makes an integration externally
// exposable; tests that exercise external endpoints use it as the definition.
const exposableDef = "env:\n  - name: HTTP_PORT\n    default: \"9090\"\n"

func TestDeployExternalThreadsIngress(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, externalEnabled: true}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Expose: ExposeExternal, Subdomain: "My Shop"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !kc.gotSpec.Expose || kc.gotSpec.Subdomain != "my-shop" {
		t.Errorf("spec expose/subdomain = %v/%q, want true/my-shop", kc.gotSpec.Expose, kc.gotSpec.Subdomain)
	}
	meta := ParseMetadata(repo.gotMetadata)
	if meta.ExternalURL == "" {
		t.Error("metadata externalUrl should be set for an external deployment")
	}
	if s := ParseSettings(repo.gotSettings); s.Expose != ExposeExternal || s.Subdomain != "my-shop" {
		t.Errorf("persisted settings = %+v, want expose external / subdomain my-shop", s)
	}
}

func TestDeployExternalDefaultsSubdomainToName(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders API", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, externalEnabled: true}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Expose: ExposeExternal}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Subdomain != "orders-api" {
		t.Errorf("subdomain = %q, want orders-api (slug of name)", kc.gotSpec.Subdomain)
	}
}

// TestDeployExternalThreadsPort checks the declared HTTP_PORT reaches the spec and
// is supplied as an env var to the runtime container.
func TestDeployExternalThreadsPort(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, externalEnabled: true}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Expose: ExposeExternal}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Port != 9090 {
		t.Errorf("spec port = %d, want 9090", kc.gotSpec.Port)
	}
	if kc.gotSpec.Env[envHTTPPort] != "9090" {
		t.Errorf("spec env %s = %q, want 9090", envHTTPPort, kc.gotSpec.Env[envHTTPPort])
	}
}

// TestDeployExternalDowngradesWhenNotExposable verifies that requesting external
// exposure for an integration that declares no HTTP_PORT silently falls back to
// internal-only rather than erroring.
func TestDeployExternalDowngradesWhenNotExposable(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{status: kube.StatusPending, externalEnabled: true}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Expose: ExposeExternal}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Expose {
		t.Error("spec should not be exposed when the integration declares no HTTP_PORT")
	}
	if s := ParseSettings(repo.gotSettings); s.Expose == ExposeExternal {
		t.Errorf("persisted settings should be internal-only, got %+v", s)
	}
}

func TestDeployExternalUnavailableWithoutBaseDomain(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{externalEnabled: false}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1", Settings{Expose: ExposeExternal})
	if !errors.Is(err, ErrExternalUnavailable) {
		t.Errorf("got %v, want ErrExternalUnavailable", err)
	}
	if kc.applied {
		t.Error("kube.Apply should not run when external is requested but unavailable")
	}
}

func TestDeployMissingIntegration(t *testing.T) {
	repo := &fakeRepo{}
	integrations := &fakeIntegrations{err: integration.ErrNotFound}
	kc := &fakeKube{}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "missing", Settings{})
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

	_, err := svc.Deploy(context.Background(), "int-1", Settings{})
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
	if _, err := svc.Deploy(context.Background(), "int-1", Settings{}); !errors.Is(err, ErrUnavailable) {
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

func TestDeployRejectsSlugTakenByAnotherIntegration(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}, slugOwner: "other-int"}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1", Settings{})
	if !errors.Is(err, ErrSlugTaken) {
		t.Errorf("got %v, want ErrSlugTaken", err)
	}
	if kc.applied {
		t.Error("kube.Apply should not run when the slug is taken")
	}
}

func TestDeployAllowsSlugOwnedBySameIntegration(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}, slugOwner: "int-1"}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders"}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{}); err != nil {
		t.Fatalf("redeploy of the same integration should be allowed, got %v", err)
	}
	if !kc.applied {
		t.Error("expected kube.Apply for a same-integration redeploy")
	}
}

func TestDeployRejectsSubdomainTakenByAnotherIntegration(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}, subdomainOwner: "other-int"}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, externalEnabled: true}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1", Settings{Expose: ExposeExternal})
	if !errors.Is(err, ErrSubdomainTaken) {
		t.Errorf("got %v, want ErrSubdomainTaken", err)
	}
	if kc.applied {
		t.Error("kube.Apply should not run when the subdomain is taken")
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
