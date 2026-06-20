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
	// takenSlugs maps an already-claimed slug to its owning integration id, so
	// allocateSlug's scan sees collisions; subdomainOwner stands in for an existing
	// deployment claiming a subdomain ("" means "not found").
	takenSlugs     map[string]string
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

func (f *fakeRepo) IntegrationIDBySlug(_ context.Context, slug string) (string, bool, error) {
	owner, ok := f.takenSlugs[slug]
	return owner, ok, nil
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
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "My Orders API!", Definition: exposableDef}}
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

// TestUndeployRemovesInternalService verifies a networked deployment's
// per-deployment internal Service is torn down with it.
func TestUndeployRemovesInternalService(t *testing.T) {
	repo := &fakeRepo{
		getRet: Deployment{ID: "dep-1", IntegrationID: "int-1", Metadata: []byte(`{"slug":"orders-001"}`)},
	}
	kc := &fakeKube{}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if err := svc.Undeploy(context.Background(), "dep-1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !kc.internalDeleted || kc.gotInternalSlug != "orders-001" {
		t.Errorf("expected internal service delete for slug orders-001 (deleted=%v slug=%q)", kc.internalDeleted, kc.gotInternalSlug)
	}
}

// TestUndeploySkipsInternalServiceWhenNoSlug verifies a non-networked deployment
// (no slug, no internal Service) needs no internal-Service cleanup.
func TestUndeploySkipsInternalServiceWhenNoSlug(t *testing.T) {
	repo := &fakeRepo{
		getRet: Deployment{ID: "dep-1", IntegrationID: "int-1", Metadata: []byte(`{}`)},
	}
	kc := &fakeKube{}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	if err := svc.Undeploy(context.Background(), "dep-1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.internalDeleted {
		t.Error("no internal service should be deleted when the deployment has no slug")
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

// TestDeployExternalDefaultsSubdomainToSlug verifies an external deploy with no
// explicit subdomain defaults its external host to the deployment's unique slug.
func TestDeployExternalDefaultsSubdomainToSlug(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders API", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending, externalEnabled: true}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Expose: ExposeExternal}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Subdomain != "orders-api" {
		t.Errorf("subdomain = %q, want orders-api (the unique slug)", kc.gotSpec.Subdomain)
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
	// A networked integration so a slug is allocated and its internal Service is
	// part of what rollback must tear down.
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{applyErr: errors.New("boom")}
	svc := NewService(repo, integrations, kc)

	_, err := svc.Deploy(context.Background(), "int-1", Settings{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !kc.deleted {
		t.Error("expected kube.Delete during rollback")
	}
	if !kc.internalDeleted || kc.gotInternalSlug != "orders" {
		t.Errorf("expected internal service cleanup during rollback (deleted=%v slug=%q)", kc.internalDeleted, kc.gotInternalSlug)
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

// TestDeployAllocatesUniqueSlugOnCollision verifies a networked deployment whose
// base slug is already claimed gets a -NNN-suffixed slug rather than colliding.
func TestDeployAllocatesUniqueSlugOnCollision(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-2"}, takenSlugs: map[string]string{"orders": "int-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Slug != "orders-001" {
		t.Errorf("spec slug = %q, want orders-001 (base taken)", kc.gotSpec.Slug)
	}
	if meta := ParseMetadata(repo.gotMetadata); meta.Slug != "orders-001" {
		t.Errorf("metadata slug = %q, want orders-001", meta.Slug)
	}
}

// TestDeployHonorsUserSlug verifies a user-supplied slug is used verbatim (after
// slugify) rather than auto-allocated.
func TestDeployHonorsUserSlug(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Slug: "My Custom API"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if kc.gotSpec.Slug != "my-custom-api" {
		t.Errorf("spec slug = %q, want my-custom-api (slugified user input)", kc.gotSpec.Slug)
	}
}

// TestDeployRejectsTakenUserSlug verifies a user-supplied slug already in use is
// rejected rather than silently incremented.
func TestDeployRejectsTakenUserSlug(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}, takenSlugs: map[string]string{"taken": "other-int"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Slug: "taken"}); !errors.Is(err, ErrSlugTaken) {
		t.Errorf("got %v, want ErrSlugTaken", err)
	}
	if kc.applied {
		t.Error("kube.Apply should not run when the chosen slug is taken")
	}
}

// TestDeployRejectsInvalidUserSlug verifies a slug with no usable DNS-1123 form is
// rejected.
func TestDeployRejectsInvalidUserSlug(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{Slug: "!!!"}); !errors.Is(err, ErrInvalidSlug) {
		t.Errorf("got %v, want ErrInvalidSlug", err)
	}
}

// TestDeployOptionsSuggestsFreeSlug verifies the no-candidate path reports the
// integration networked and suggests a free slug.
func TestDeployOptionsSuggestsFreeSlug(t *testing.T) {
	repo := &fakeRepo{takenSlugs: map[string]string{"orders": "other-int"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	svc := NewService(repo, integrations, &fakeKube{})

	opts, err := svc.DeployOptions(context.Background(), "int-1", "", false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !opts.Networked || opts.SuggestedSlug != "orders-001" {
		t.Errorf("opts = %+v, want networked with suggested orders-001", opts)
	}
}

// TestDeployOptionsValidatesSubdomainOnlyWhenExternal verifies a candidate free as
// a slug but taken as a subdomain is available for internal use but not external.
func TestDeployOptionsValidatesSubdomainOnlyWhenExternal(t *testing.T) {
	repo := &fakeRepo{subdomainOwner: "other-int"} // "orders" free as a slug, taken as a subdomain
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Orders", Definition: exposableDef}}
	svc := NewService(repo, integrations, &fakeKube{})

	internal, err := svc.DeployOptions(context.Background(), "int-1", "orders", false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !internal.SlugChecked || !internal.SlugValid || !internal.SlugAvailable {
		t.Errorf("internal opts = %+v, want available (subdomain irrelevant)", internal)
	}

	external, err := svc.DeployOptions(context.Background(), "int-1", "orders", true)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !external.SlugValid || external.SlugAvailable {
		t.Errorf("external opts = %+v, want unavailable (subdomain taken)", external)
	}
}

// TestDeployOptionsNonNetworked verifies a non-networked integration reports no
// slug machinery at all.
func TestDeployOptionsNonNetworked(t *testing.T) {
	repo := &fakeRepo{}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Daily Job", Definition: "service:\n  name: daily\n"}}
	svc := NewService(repo, integrations, &fakeKube{})

	opts, err := svc.DeployOptions(context.Background(), "int-1", "", false)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if opts.Networked || opts.SuggestedSlug != "" {
		t.Errorf("opts = %+v, want non-networked with no suggestion", opts)
	}
}

// TestDeployNonNetworkedSkipsSlug verifies an integration with no HTTP source
// (no HTTP_PORT) gets no slug and no internal URL — no Service is created for it.
func TestDeployNonNetworkedSkipsSlug(t *testing.T) {
	repo := &fakeRepo{created: Deployment{ID: "dep-1"}}
	integrations := &fakeIntegrations{ret: integration.Integration{ID: "int-1", Name: "Daily Job", Definition: "service:\n  name: daily\n"}}
	kc := &fakeKube{status: kube.StatusPending}
	svc := NewService(repo, integrations, kc)

	if _, err := svc.Deploy(context.Background(), "int-1", Settings{}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !kc.applied {
		t.Error("expected kube.Apply for a non-networked deployment")
	}
	if kc.gotSpec.Slug != "" {
		t.Errorf("spec slug = %q, want empty (no HTTP source)", kc.gotSpec.Slug)
	}
	meta := ParseMetadata(repo.gotMetadata)
	if meta.Slug != "" || meta.InternalURL != "" {
		t.Errorf("metadata slug/url should be empty for a non-networked deployment: %+v", meta)
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
