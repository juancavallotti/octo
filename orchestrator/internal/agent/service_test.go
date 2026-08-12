package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentapp "github.com/juancavallotti/octo/orchestrator/agent"
	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/llm"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
	"github.com/juancavallotti/octo/orchestrator/internal/secret"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// ---------------------------------------------------------------- fakes

type fakeRepo struct{ row stored }

func (f *fakeRepo) Get(context.Context) (stored, error) { return f.row, nil }

func (f *fakeRepo) Mutate(_ context.Context, fn func(stored) (stored, error)) error {
	next, err := fn(f.row)
	if err != nil {
		return err
	}
	f.row = next
	return nil
}

type fakeIntegrations struct {
	items    map[string]integration.Integration
	creates  int
	updates  int
	deletes  int
	nextID   int
	createFn func(name, definition string) error
}

func newFakeIntegrations() *fakeIntegrations {
	return &fakeIntegrations{items: map[string]integration.Integration{}}
}

func (f *fakeIntegrations) Create(_ context.Context, name, definition, _ string) (integration.Integration, error) {
	if f.createFn != nil {
		if err := f.createFn(name, definition); err != nil {
			return integration.Integration{}, err
		}
	}
	f.creates++
	f.nextID++
	id := "int-" + string(rune('a'+f.nextID-1))
	it := integration.Integration{ID: id, Name: name, Definition: definition}
	f.items[id] = it
	return it, nil
}

func (f *fakeIntegrations) Get(_ context.Context, id string) (integration.Integration, error) {
	it, ok := f.items[id]
	if !ok {
		return integration.Integration{}, integration.ErrNotFound
	}
	return it, nil
}

func (f *fakeIntegrations) List(context.Context) ([]integration.Integration, error) {
	out := make([]integration.Integration, 0, len(f.items))
	for _, it := range f.items {
		out = append(out, it)
	}
	return out, nil
}

func (f *fakeIntegrations) Update(_ context.Context, id, name, definition, _ string) (integration.Integration, error) {
	f.updates++
	it := integration.Integration{ID: id, Name: name, Definition: definition}
	f.items[id] = it
	return it, nil
}

func (f *fakeIntegrations) Delete(_ context.Context, id string) error {
	f.deletes++
	delete(f.items, id)
	return nil
}

type fakeResources struct {
	items   map[string]resource.Resource // keyed by name
	creates int
	updates int
	deletes int
}

func newFakeResources() *fakeResources {
	return &fakeResources{items: map[string]resource.Resource{}}
}

func (f *fakeResources) Create(_ context.Context, integrationID, kind, name, content string) (resource.Resource, error) {
	f.creates++
	res := resource.Resource{
		ID: "res-" + name, IntegrationID: integrationID, Kind: kind, Name: name, Content: content,
	}
	f.items[name] = res
	return res, nil
}

func (f *fakeResources) Update(_ context.Context, integrationID, id, kind, name, content string) (resource.Resource, error) {
	f.updates++
	res := resource.Resource{
		ID: id, IntegrationID: integrationID, Kind: kind, Name: name, Content: content,
	}
	f.items[name] = res
	return res, nil
}

func (f *fakeResources) Delete(_ context.Context, _, id string) error {
	f.deletes++
	for name, item := range f.items {
		if item.ID == id {
			delete(f.items, name)
			return nil
		}
	}
	return nil
}

func (f *fakeResources) ListByIntegration(context.Context, string) ([]resource.Resource, error) {
	out := make([]resource.Resource, 0, len(f.items))
	for _, item := range f.items {
		out = append(out, item)
	}
	return out, nil
}

type fakeSnapshots struct {
	items   []snapshot.Snapshot
	creates int
}

func (f *fakeSnapshots) Create(_ context.Context, integrationID, tag string) (snapshot.Snapshot, error) {
	f.creates++
	snap := snapshot.Snapshot{ID: "snap-" + tag, IntegrationID: integrationID, Tag: tag}
	f.items = append(f.items, snap)
	return snap, nil
}

func (f *fakeSnapshots) ListByIntegration(context.Context, string) ([]snapshot.Snapshot, error) {
	return f.items, nil
}

// rolloutCall records one Rollout, which is the whole assertion for tracing.
type rolloutCall struct {
	deploymentID string
	snapshotID   string
	env          map[string]deployment.EnvBinding
	tracing      *bool
}

type fakeDeployments struct {
	deployed  []deployment.Settings
	rollouts  []rolloutCall
	undeploys int
	status    string
	reason    string
	getErr    error
}

func (f *fakeDeployments) Deploy(_ context.Context, integrationID string, s deployment.Settings) (deployment.Deployment, error) {
	f.deployed = append(f.deployed, s)
	meta, _ := json.Marshal(deployment.Metadata{
		Name: agentapp.Name, Slug: "dr-octo", InternalURL: "http://octo-int-dr-octo.octo:8080",
	})
	return deployment.Deployment{ID: "dep-1", IntegrationID: integrationID, Metadata: meta}, nil
}

func (f *fakeDeployments) Rollout(
	_ context.Context, id, snapshotID string, env map[string]deployment.EnvBinding, tracing *bool,
) (deployment.Deployment, error) {
	f.rollouts = append(f.rollouts, rolloutCall{id, snapshotID, env, tracing})
	meta, _ := json.Marshal(deployment.Metadata{InternalURL: "http://octo-int-dr-octo.octo:8080"})
	return deployment.Deployment{ID: id, Metadata: meta}, nil
}

func (f *fakeDeployments) Get(context.Context, string) (deployment.Deployment, error) {
	if f.getErr != nil {
		return deployment.Deployment{}, f.getErr
	}
	status := f.status
	if status == "" {
		status = kube.StatusRunning
	}
	return deployment.Deployment{
		ID: "dep-1", Status: status, Detail: kube.Status{Reason: f.reason},
	}, nil
}

func (f *fakeDeployments) Undeploy(context.Context, string) error {
	f.undeploys++
	return nil
}

type fakeSecrets struct {
	written map[string]string
	deleted []string
}

// Create enforces the real service's name rule. A fake that accepted any name is
// how "octo-agent-llm-key" reached a cluster and was refused there: every test
// below passed, because the only thing that knew the rule was the code the tests
// had replaced. A fake may be simple, but it may not be more permissive than the
// thing it stands in for.
func (f *fakeSecrets) Create(_ context.Context, name, value string) (secret.Secret, error) {
	if !secret.ValidName(name) {
		return secret.Secret{}, secret.ErrInvalidName
	}
	if f.written == nil {
		f.written = map[string]string{}
	}
	f.written[name] = value
	return secret.Secret{Name: name}, nil
}

type fakeCredentials struct {
	creds    llm.Credentials
	err      error
	noCipher bool
	reveals  int
}

func (f *fakeSecrets) Delete(_ context.Context, name string, _ bool) error {
	f.deleted = append(f.deleted, name)
	delete(f.written, name)
	return nil
}

func (f *fakeCredentials) Get(context.Context) (llm.Settings, error) {
	if f.err != nil {
		return llm.Settings{}, f.err
	}
	return llm.Settings{
		Provider:   f.creds.Provider,
		Model:      f.creds.Model,
		Configured: f.creds.APIKey != "",
	}, nil
}

func (f *fakeCredentials) EncryptionAvailable() bool { return !f.noCipher }

func (f *fakeCredentials) Reveal(context.Context) (llm.Credentials, error) {
	f.reveals++
	if f.err != nil {
		return llm.Credentials{}, f.err
	}
	return f.creds, nil
}

// ---------------------------------------------------------------- harness

type harness struct {
	svc          *Service
	repo         *fakeRepo
	integrations *fakeIntegrations
	resources    *fakeResources
	snapshots    *fakeSnapshots
	deployments  *fakeDeployments
	secrets      *fakeSecrets
	credentials  *fakeCredentials
}

// newHarness wires the service against fakes, with a cluster unless withCluster is
// false — which is how the "local go run" case is exercised.
func newHarness(t *testing.T, withCluster bool) *harness {
	t.Helper()
	h := &harness{
		repo:         &fakeRepo{},
		integrations: newFakeIntegrations(),
		resources:    newFakeResources(),
		snapshots:    &fakeSnapshots{},
		deployments:  &fakeDeployments{},
		secrets:      &fakeSecrets{},
		credentials: &fakeCredentials{creds: llm.Credentials{
			Provider: llm.ProviderAnthropic, Model: "claude-sonnet-4-6", APIKey: "sk-ant-test",
		}},
	}
	var opts []Option
	if withCluster {
		opts = append(opts, WithCluster(h.deployments, h.secrets))
	}
	h.svc = NewService(
		h.repo, h.integrations, h.resources, h.snapshots, h.credentials,
		"http://octo-orchestrator:8090", opts...,
	)
	return h
}

// ---------------------------------------------------------------- status

func TestStatusIsNotInstalledOnAFreshInstall(t *testing.T) {
	h := newHarness(t, true)

	got, err := h.svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != StateNotInstalled {
		t.Errorf("state = %q, want %q", got.State, StateNotInstalled)
	}
	if got.Blocked != "" {
		t.Errorf("blocked = %q, want nothing in the way", got.Blocked)
	}
	if got.BundleDigest == "" {
		t.Error("want the bundle digest reported even before an install")
	}
}

// Without a cluster the status still has to answer, and say why nothing can be
// done. A route that disappeared would leave the admin page with a 404 to interpret.
func TestStatusReportsTheBlockageWithoutACluster(t *testing.T) {
	h := newHarness(t, false)

	got, err := h.svc.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Blocked != BlockedKubernetes {
		t.Errorf("blocked = %q, want %q", got.Blocked, BlockedKubernetes)
	}
}

func TestStatusDistinguishesAMissingKeyFromMissingEncryption(t *testing.T) {
	t.Run("no key stored", func(t *testing.T) {
		h := newHarness(t, true)
		h.credentials.creds.APIKey = ""

		got, err := h.svc.Status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if got.Blocked != BlockedLLMKey {
			t.Errorf("blocked = %q, want %q", got.Blocked, BlockedLLMKey)
		}
	})

	t.Run("a key stored that cannot be decrypted", func(t *testing.T) {
		h := newHarness(t, true)
		h.credentials.noCipher = true

		got, err := h.svc.Status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if got.Blocked != BlockedEncryption {
			t.Errorf("blocked = %q, want %q", got.Blocked, BlockedEncryption)
		}
	})
}

// Reading a status is a poll. Decrypting a provider key to ask whether one exists
// would put the plaintext in memory on every one of them.
func TestStatusNeverDecryptsTheProviderKey(t *testing.T) {
	h := newHarness(t, true)

	if _, err := h.svc.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if h.credentials.reveals != 0 {
		t.Errorf("Reveal called %d times reading a status; want none", h.credentials.reveals)
	}
}

// ---------------------------------------------------------------- install

func TestInstallCreatesTheIntegrationItsSkillsATagAndADeployment(t *testing.T) {
	h := newHarness(t, true)

	got, err := h.svc.Install(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	if h.integrations.creates != 1 {
		t.Errorf("integration creates = %d, want 1", h.integrations.creates)
	}
	if h.integrations.items[got.IntegrationID].Name != agentapp.Name {
		t.Errorf("integration name = %q, want %q",
			h.integrations.items[got.IntegrationID].Name, agentapp.Name)
	}

	skills, err := agentapp.Skills()
	if err != nil {
		t.Fatalf("Skills: %v", err)
	}
	if h.resources.creates != len(skills) {
		t.Errorf("resource creates = %d, want one per skill (%d)", h.resources.creates, len(skills))
	}
	for name := range skills {
		if _, ok := h.resources.items[name]; !ok {
			t.Errorf("skill %q was not written as a resource", name)
		}
	}

	if h.snapshots.creates != 1 {
		t.Errorf("snapshot creates = %d, want 1", h.snapshots.creates)
	}
	if len(h.deployments.deployed) != 1 {
		t.Fatalf("deploys = %d, want 1", len(h.deployments.deployed))
	}
	if got.State != StateDeployed {
		t.Errorf("state = %q, want %q", got.State, StateDeployed)
	}
	if got.InternalURL == "" {
		t.Error("want the deployment's internal address recorded")
	}
}

// The version tag is derived from the bundle, so re-installing the same binary
// finds the tag it already made rather than failing on an immutable one.
func TestInstallIsIdempotent(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, "user-1"); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	before := h.resources.creates

	if _, err := h.svc.Install(ctx, "user-1"); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	if h.integrations.creates != 1 {
		t.Errorf("integration creates = %d, want the existing one reused", h.integrations.creates)
	}
	if h.snapshots.creates != 1 {
		t.Errorf("snapshot creates = %d, want the existing tag reused", h.snapshots.creates)
	}
	if h.resources.creates != before {
		t.Errorf("resource creates = %d, want no duplicates (%d)", h.resources.creates, before)
	}
}

func TestInstallTagsFromTheBundleDigest(t *testing.T) {
	h := newHarness(t, true)

	got, err := h.svc.Install(context.Background(), "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	digest, err := agentapp.Digest()
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if got.InstalledTag != agentapp.Tag(digest) {
		t.Errorf("tag = %q, want %q", got.InstalledTag, agentapp.Tag(digest))
	}
}

// The key goes to a cluster secret and the binding carries only its name — the same
// contract every other secret binding has, and the reason a deployment record never
// holds a credential.
func TestInstallBindsTheProviderKeyThroughAClusterSecret(t *testing.T) {
	h := newHarness(t, true)

	if _, err := h.svc.Install(context.Background(), ""); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if got := h.secrets.written[llmKeySecret]; got != "sk-ant-test" {
		t.Errorf("cluster secret = %q, want the provider key written to it", got)
	}

	env := h.deployments.deployed[0].Env
	if env[envAPIKey].Secret != llmKeySecret {
		t.Errorf("%s binding = %+v, want a secret reference", envAPIKey, env[envAPIKey])
	}
	if env[envAPIKey].Value != "" {
		t.Error("the key itself must not appear in the deployment settings")
	}
	if env[envConnectorType].Value != "llm-anthropic" {
		t.Errorf("%s = %q, want the provider mapped to a connector type",
			envConnectorType, env[envConnectorType].Value)
	}
	if env[envModel].Value != "claude-sonnet-4-6" {
		t.Errorf("%s = %q, want the configured model", envModel, env[envModel].Value)
	}
	// The orchestrator injects this into every pod anyway, but the deploy-time check
	// for required variables sees only bindings — so an unbound one is refused before
	// the pod exists.
	if env[envOrchestrator].Value == "" {
		t.Errorf("%s must be bound explicitly", envOrchestrator)
	}
}

func TestInstallDeploysInternalOnly(t *testing.T) {
	h := newHarness(t, true)

	if _, err := h.svc.Install(context.Background(), ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := h.deployments.deployed[0].Expose; got != "" {
		t.Errorf("expose = %q, want the agent internal-only", got)
	}
}

// The agent asks for the platform-access grants the same way any integration does.
// Observability is the one that does something: it is what puts LOGS_URL in his pod,
// and without it his logs tools answer that they were not granted rather than that
// there is nothing stored.
func TestInstallAsksForThePlatformAccessGrants(t *testing.T) {
	h := newHarness(t, true)

	if _, err := h.svc.Install(context.Background(), ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got := h.deployments.deployed[0]
	if !got.ObservabilityAPI {
		t.Error("want the observability grant; without it he cannot read stored logs or traces")
	}
	if !got.OrchestratorAPI {
		t.Error("want the orchestrator grant declared; he drives that API on every turn")
	}
}

func TestInstallRefusesWithoutACluster(t *testing.T) {
	h := newHarness(t, false)

	_, err := h.svc.Install(context.Background(), "")
	if !errors.Is(err, ErrClusterUnavailable) {
		t.Fatalf("error = %v, want ErrClusterUnavailable", err)
	}
	if h.integrations.creates != 0 {
		t.Error("want nothing created when the install cannot finish")
	}
}

func TestInstallSurfacesAMissingProviderKey(t *testing.T) {
	h := newHarness(t, true)
	h.credentials.err = sitesettings.ErrNotConfigured

	_, err := h.svc.Install(context.Background(), "")
	if !errors.Is(err, sitesettings.ErrNotConfigured) {
		t.Fatalf("error = %v, want the missing key reported as itself", err)
	}
}

// A provider the orchestrator cannot map to a connector is refused rather than
// guessed at: the fix is in the LLM settings, and a wrong guess fails in the pod.
func TestInstallRefusesAnUnmappableProvider(t *testing.T) {
	h := newHarness(t, true)
	h.credentials.creds.Provider = "MISTRAL"

	_, err := h.svc.Install(context.Background(), "")
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("error = %v, want ErrUnknownProvider", err)
	}
}

func TestConnectorTypeForCoversEveryProviderTheSettingsAccept(t *testing.T) {
	for _, provider := range []string{llm.ProviderAnthropic, llm.ProviderOpenAI, llm.ProviderGoogle} {
		got, ok := ConnectorTypeFor(provider)
		if !ok {
			t.Errorf("ConnectorTypeFor(%q) is unmapped; the LLM settings accept it", provider)
			continue
		}
		if !strings.HasPrefix(got, "llm-") {
			t.Errorf("ConnectorTypeFor(%q) = %q, want an llm connector type", provider, got)
		}
	}
	if _, ok := ConnectorTypeFor("NOPE"); ok {
		t.Error("want an unknown provider refused")
	}
}

// ---------------------------------------------------------------- updates

func TestANewerBundleReportsAnUpdateWithoutTouchingTheDeployment(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// Stand in for a newer binary: the record remembers a different bundle.
	h.repo.row.InstalledDigest = "0000000000000000000000000000000000000000000000000000000000000000"

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !got.UpdateAvailable {
		t.Error("want an update reported when the bundle differs")
	}
	if got.State != StateUpdateAvailable {
		t.Errorf("state = %q, want %q", got.State, StateUpdateAvailable)
	}
	if len(h.deployments.rollouts) != 0 {
		t.Error("reading the status must not change anything")
	}
}

func TestRolloutPublishesTheBundleAndRollsTheDeployment(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	h.repo.row.InstalledDigest = "stale"

	got, err := h.svc.Rollout(ctx, "user-1")
	if err != nil {
		t.Fatalf("Rollout: %v", err)
	}

	if h.integrations.updates != 1 {
		t.Errorf("integration updates = %d, want the definition republished", h.integrations.updates)
	}
	if len(h.deployments.rollouts) != 1 {
		t.Fatalf("rollouts = %d, want 1", len(h.deployments.rollouts))
	}
	if got.UpdateAvailable {
		t.Error("want no update outstanding after rolling out")
	}
	// Env is re-resolved and passed, so a provider or model changed in the LLM
	// settings since the install takes effect on the roll-out.
	if h.deployments.rollouts[0].env[envModel].Value != "claude-sonnet-4-6" {
		t.Error("want the environment re-resolved on rollout")
	}
	// Tracing is the deployment's own setting; a version bump must not disturb it.
	if h.deployments.rollouts[0].tracing != nil {
		t.Error("want rollout to leave tracing alone")
	}
}

// A roll-out overwrites the working copy with the shipped bundle, so whatever was
// there is gone the moment republish runs. Freezing it first is the only thing that
// makes "your changes stop running" different from "your changes are destroyed",
// and it is what the confirmation now promises.
func TestRolloutFreezesTheEditedDefinitionBeforeReplacingIt(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	installed, err := h.svc.Install(ctx, "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	tagsAfterInstall := len(h.snapshots.items)

	// Edit the agent the way the editor does: change the working copy in place.
	it := h.integrations.items[installed.IntegrationID]
	edited := it.Definition + "\n# someone changed the prompt\n"
	it.Definition = edited
	h.integrations.items[installed.IntegrationID] = it

	if _, err := h.svc.Rollout(ctx, "user-1"); err != nil {
		t.Fatalf("Rollout: %v", err)
	}

	var preserved *snapshot.Snapshot
	for i, snap := range h.snapshots.items {
		if strings.HasPrefix(snap.Tag, "agent-edited-") {
			preserved = &h.snapshots.items[i]
		}
	}
	if preserved == nil {
		t.Fatalf("want the edited definition frozen under an agent-edited- tag, got %v",
			h.snapshots.items)
	}
	if len(h.snapshots.items) != tagsAfterInstall+1 {
		t.Errorf("snapshots = %d, want exactly one added for the edits", len(h.snapshots.items))
	}
	// The working copy is stock again, which is the point of the roll-out — the
	// edits live in the tag above and nowhere else.
	if got := h.integrations.items[installed.IntegrationID].Definition; got == edited {
		t.Error("want the working copy replaced by the shipped bundle")
	}
}

// A roll-out with nothing to preserve must not mint a version per press. The button
// is now always offered, so a redeploy of an unedited agent is an ordinary thing to
// do repeatedly.
func TestRolloutFreezesNothingWhenTheAgentIsUnedited(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	before := len(h.snapshots.items)

	for i := range 2 {
		if _, err := h.svc.Rollout(ctx, "user-1"); err != nil {
			t.Fatalf("Rollout %d: %v", i, err)
		}
	}

	if got := len(h.snapshots.items); got != before {
		t.Errorf("snapshots = %d, want %d — an unedited roll-out has nothing to freeze",
			got, before)
	}
}

// Editing the agent is supported, so the status has to say when it has happened —
// a roll-out replaces those edits, and that should be a known choice.
func TestStatusReportsAnEditedIntegration(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	installed, err := h.svc.Install(ctx, "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if installed.Edited {
		t.Fatal("a freshly installed agent is not edited")
	}

	it := h.integrations.items[installed.IntegrationID]
	it.Definition += "\n# someone changed the prompt\n"
	h.integrations.items[installed.IntegrationID] = it

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !got.Edited {
		t.Error("want an edited definition reported")
	}
}

// A user's own template is theirs, and a roll-out does not remove it — so it must
// not read as an edit to the agent.
func TestAUserAddedResourceIsNotAnEdit(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	installed, err := h.svc.Install(ctx, "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := h.resources.Create(ctx, installed.IntegrationID,
		"template", "templates/mine.tmpl", "hello"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Edited {
		t.Error("a resource the bundle does not own must not count as an edit")
	}
}

func TestStatusReportsAnEditedSkill(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	installed, err := h.svc.Install(ctx, "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for name, item := range h.resources.items {
		item.Content += "\nlocal note\n"
		h.resources.items[name] = item
		break
	}

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !got.Edited {
		t.Errorf("want an edited skill reported (integration %s)", installed.IntegrationID)
	}
}

// ---------------------------------------------------------------- tracing

func TestSetTracingRollsTheCurrentTagWithTracingOnly(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	installed, err := h.svc.Install(ctx, "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := h.svc.SetTracing(ctx, true)
	if err != nil {
		t.Fatalf("SetTracing: %v", err)
	}

	if len(h.deployments.rollouts) != 1 {
		t.Fatalf("rollouts = %d, want 1", len(h.deployments.rollouts))
	}
	call := h.deployments.rollouts[0]
	if call.snapshotID != h.repo.row.SnapshotID {
		t.Errorf("snapshotId = %q, want the tag already running (%q)", call.snapshotID, h.repo.row.SnapshotID)
	}
	if call.env != nil {
		t.Error("want a nil env so the deployment's bindings are preserved")
	}
	if call.tracing == nil || !*call.tracing {
		t.Errorf("tracing = %v, want it set on", call.tracing)
	}
	if !got.Tracing {
		t.Error("want the status to report tracing on")
	}
	if installed.Tracing {
		t.Error("tracing should have been off before")
	}
}

func TestSetTracingRefusesWhenNothingIsInstalled(t *testing.T) {
	h := newHarness(t, true)

	_, err := h.svc.SetTracing(context.Background(), true)
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("error = %v, want ErrNotInstalled", err)
	}
}

// ---------------------------------------------------------------- uninstall

func TestUninstallRemovesTheDeploymentAndKeepsTheIntegration(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := h.svc.Uninstall(ctx, false); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if h.deployments.undeploys != 1 {
		t.Errorf("undeploys = %d, want 1", h.deployments.undeploys)
	}
	if h.integrations.deletes != 0 {
		t.Error("want the integration kept: it holds a definition that may have been edited")
	}
	if h.repo.row.DeploymentID != "" {
		t.Error("want the deployment forgotten")
	}
	if h.repo.row.IntegrationID == "" {
		t.Error("want the integration still recorded")
	}
}

func TestUninstallWithPurgeForgetsEverything(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := h.svc.Uninstall(ctx, true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if h.integrations.deletes != 1 {
		t.Errorf("integration deletes = %d, want 1", h.integrations.deletes)
	}
	// A digest left behind would report an update available for an agent that is
	// not installed.
	if h.repo.row.InstalledDigest != "" {
		t.Error("want the record cleared with the install")
	}

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != StateNotInstalled {
		t.Errorf("state = %q, want %q", got.State, StateNotInstalled)
	}
}

// ---------------------------------------------------------------- failure

func TestStatusReportsAFailedDeployment(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	h.deployments.status = kube.StatusFailed
	h.deployments.reason = "ImagePullBackOff"

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != StateFailed {
		t.Errorf("state = %q, want %q", got.State, StateFailed)
	}
	if got.Reason != "ImagePullBackOff" {
		t.Errorf("reason = %q, want the cluster's own", got.Reason)
	}
}

// A deployment removed underneath the record is the install being back at "not
// running", not an error to hand a page that wanted a status.
func TestStatusToleratesAVanishedDeployment(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	h.deployments.getErr = errors.New("not found")

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.State != StateDeployed && got.State != StateInstalled {
		t.Errorf("state = %q, want the install still reported", got.State)
	}
}

// A deploy that fails on a transient error must not wedge the feature. The
// integration is created through its own connection and commits regardless, so the
// id is recorded first — and a retry reuses it instead of colliding with the name
// the installer itself took.
func TestAFailedDeployLeavesTheInstallRetryable(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	h.credentials.err = errors.New("the api server hiccuped")
	if _, err := h.svc.Install(ctx, "user-1"); err == nil {
		t.Fatal("want the install to fail")
	}
	if h.repo.row.IntegrationID == "" {
		t.Fatal("want the integration recorded even though the install failed")
	}

	h.credentials.err = nil
	got, err := h.svc.Install(ctx, "user-1")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if h.integrations.creates != 1 {
		t.Errorf("integration creates = %d, want the first one reused", h.integrations.creates)
	}
	if got.State != StateDeployed {
		t.Errorf("state = %q, want the retry to complete", got.State)
	}
}

// The residual window: created, and the row write lost. Adopting is right, and
// better than telling the operator to rename an integration they did not make.
func TestInstallAdoptsAnOrphanedIntegration(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	definition, err := agentapp.Definition()
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if _, err := h.integrations.Create(ctx, agentapp.Name, definition, "someone"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h.integrations.createFn = func(string, string) error { return integration.ErrNameTaken }

	got, err := h.svc.Install(ctx, "user-1")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got.IntegrationID == "" {
		t.Fatal("want the orphan adopted")
	}
	if got.State != StateDeployed {
		t.Errorf("state = %q, want the install to complete", got.State)
	}
}

// An integration a user happens to have named "Dr. Octo" is theirs. Adopting it
// would snapshot and deploy their work and replace it on the next roll-out, with
// Edited in the status as the only warning — after the fact. The name alone is not
// enough; the definition has to be recognisably the agent's.
func TestInstallDoesNotAdoptAUsersOwnIntegration(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.integrations.Create(ctx, agentapp.Name,
		"service:\n  name: my-own-thing\n", "someone"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	h.integrations.createFn = func(string, string) error { return integration.ErrNameTaken }

	_, err := h.svc.Install(ctx, "user-1")
	if !errors.Is(err, integration.ErrNameTaken) {
		t.Fatalf("error = %v, want the name collision reported so the operator decides", err)
	}
	if len(h.deployments.deployed) != 0 {
		t.Error("want nothing deployed from someone else's integration")
	}
}

// A skill the bundle stops shipping has to go, or liveDigest keeps counting it
// while the bundle digest does not — leaving the agent permanently "edited" with no
// roll-out able to clear it.
func TestRolloutRemovesASkillTheBundleNoLongerShips(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	installed, err := h.svc.Install(ctx, "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := h.resources.Create(ctx, installed.IntegrationID,
		agentapp.SkillResourceKind, "skills/retired.md", "an older bundle shipped this"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	stale, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !stale.Edited {
		t.Fatal("a leftover skill should read as edited until it is removed")
	}

	if _, err := h.svc.Rollout(ctx, ""); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	if _, ok := h.resources.items["skills/retired.md"]; ok {
		t.Error("want the retired skill removed")
	}

	got, err := h.svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Edited {
		t.Error("want the roll-out to have cleared the edit")
	}
}

// The credential is the part of an install that most needs to go. Left behind it is
// a plaintext provider key in the cluster with nothing owning it.
func TestPurgeRemovesTheProviderKeySecret(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := h.svc.Uninstall(ctx, true); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if len(h.secrets.deleted) == 0 || h.secrets.deleted[0] != llmKeySecret {
		t.Errorf("deleted secrets = %v, want %q removed", h.secrets.deleted, llmKeySecret)
	}
	if _, ok := h.secrets.written[llmKeySecret]; ok {
		t.Error("want the key gone from the cluster")
	}
}

// Undeploying without purge keeps the integration, so it keeps the key it will need
// when it is deployed again.
func TestUninstallWithoutPurgeKeepsTheProviderKeySecret(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	if _, err := h.svc.Install(ctx, ""); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := h.svc.Uninstall(ctx, false); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(h.secrets.deleted) != 0 {
		t.Errorf("deleted secrets = %v, want none", h.secrets.deleted)
	}
}

// Rolling out with nothing running still has to publish the shipped definition, or
// an operator who asked for the shipped agent gets their own edits frozen into the
// new tag instead.
func TestRolloutRepublishesEvenWhenNothingIsDeployed(t *testing.T) {
	h := newHarness(t, true)
	ctx := context.Background()

	installed, err := h.svc.Install(ctx, "")
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if err := h.svc.Uninstall(ctx, false); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	it := h.integrations.items[installed.IntegrationID]
	edited := it.Definition + "\n# a local change\n"
	it.Definition = edited
	h.integrations.items[installed.IntegrationID] = it

	if _, err := h.svc.Rollout(ctx, "user-1"); err != nil {
		t.Fatalf("Rollout: %v", err)
	}
	if h.integrations.items[installed.IntegrationID].Definition == edited {
		t.Error("want the shipped definition republished, not the local edit kept")
	}
}

// An empty URL satisfies the deploy-time check for required variables and then
// fails on the agent's first call back. Refusing while someone is looking is the
// whole point.
func TestInstallRefusesWithoutAnOrchestratorURL(t *testing.T) {
	h := newHarness(t, true)
	h.svc.orchestratorURL = ""

	_, err := h.svc.Install(context.Background(), "")
	if !errors.Is(err, ErrNoOrchestratorURL) {
		t.Fatalf("error = %v, want ErrNoOrchestratorURL", err)
	}
	if len(h.deployments.deployed) != 0 {
		t.Error("want nothing deployed")
	}
}

// The name is a constant in this package and the rule that governs it lives in
// another, so nothing connected the two until an install reached a real cluster
// and was refused. This is that connection: change the rule or the constant and
// one of them has to give.
func TestLLMKeySecretIsAValidPlatformSecretName(t *testing.T) {
	if !secret.ValidName(llmKeySecret) {
		t.Fatalf("llmKeySecret = %q, which the secrets catalogue refuses; "+
			"a platform secret name is UPPER_SNAKE_CASE, because it is a data key "+
			"in the shared Secret and not a Secret object of its own", llmKeySecret)
	}
}

// Every environment name the installer binds is also an env var in the agent's
// own config.yaml, and the same rule applies to all of them.
func TestBoundEnvironmentNamesAreValid(t *testing.T) {
	for _, name := range []string{envConnectorType, envModel, envAPIKey, envOrchestrator} {
		if !secret.ValidName(name) {
			t.Errorf("env name %q is not a valid environment variable name", name)
		}
	}
}
