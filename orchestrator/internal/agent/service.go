package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	agentapp "github.com/juancavallotti/octo/orchestrator/agent"
	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/llm"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
	"github.com/juancavallotti/octo/orchestrator/internal/secret"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// The collaborators, declared here rather than imported as concrete services so the
// whole install can be exercised without a database or a cluster. Each is the
// subset of a real service this package actually calls.
type (
	repository interface {
		Get(ctx context.Context) (stored, error)
		Mutate(ctx context.Context, fn func(current stored) (stored, error)) error
	}

	integrations interface {
		Create(ctx context.Context, name, definition, actorID string) (integration.Integration, error)
		Get(ctx context.Context, id string) (integration.Integration, error)
		List(ctx context.Context) ([]integration.Integration, error)
		Update(ctx context.Context, id, name, definition, actorID string) (integration.Integration, error)
		Delete(ctx context.Context, id string) error
	}

	resources interface {
		Create(ctx context.Context, integrationID, kind, name, content, actorID string) (resource.Resource, error)
		Update(ctx context.Context, integrationID, id, kind, name, content, actorID string) (resource.Resource, error)
		Delete(ctx context.Context, integrationID, id string) error
		ListByIntegration(ctx context.Context, integrationID string) ([]resource.Resource, error)
	}

	snapshots interface {
		Create(ctx context.Context, integrationID, tag string) (snapshot.Snapshot, error)
		ListByIntegration(ctx context.Context, integrationID string) ([]snapshot.Snapshot, error)
	}

	// deployer is nil when the orchestrator has no cluster access, which is why
	// every method that needs it checks first rather than assuming.
	deployer interface {
		Deploy(ctx context.Context, integrationID string, settings deployment.Settings) (deployment.Deployment, error)
		Rollout(
			ctx context.Context, id, snapshotID string,
			env map[string]deployment.EnvBinding, tracing *bool, runner *string,
		) (deployment.Deployment, error)
		Get(ctx context.Context, id string) (deployment.Deployment, error)
		Undeploy(ctx context.Context, id string) error
		RunnerAvailable(runner string) bool
	}

	secrets interface {
		Create(ctx context.Context, name, value string) (secret.Secret, error)
		Delete(ctx context.Context, name string, force bool) error
	}

	// credentials is llm.Service. Reveal decrypts, so only install and rollout call
	// it; Get and EncryptionAvailable answer "is one configured?" without ever
	// materialising the key.
	credentials interface {
		Get(ctx context.Context) (llm.Settings, error)
		EncryptionAvailable() bool
		Reveal(ctx context.Context) (llm.Credentials, error)
	}

	// webSearch is websearch.Service. Only Reveal, because this credential blocks
	// no install and changes no status. An unconfigured install returns the empty
	// string rather than an error, which keeps "no key" an ordinary path.
	webSearch interface {
		Reveal(ctx context.Context) (string, error)
	}
)

// Service installs and reports on the agent.
type Service struct {
	repo         repository
	integrations integrations
	resources    resources
	snapshots    snapshots
	deployments  deployer
	secrets      secrets
	credentials  credentials
	// webSearch is nil where it was never wired. Nil binds the sentinel, which is
	// exactly what an unconfigured key does.
	webSearch webSearch

	// orchestratorURL is bound on the deployment as ORCHESTRATOR_URL. The same value
	// is injected into every pod, but the deploy-time check for required variables
	// sees only bindings, so a required declaration with no binding is refused before
	// the pod exists.
	orchestratorURL string
}

// Option configures a Service.
type Option func(*Service)

// WithCluster supplies the half of the service that needs Kubernetes.
//
// An option rather than two more parameters, so that "no cluster" is the absence of
// a call rather than two nils passed in: a nil *deployment.Service in an interface
// parameter is a non-nil interface holding a nil pointer, and the nil checks inside
// would pass.
func WithCluster(deployments deployer, secrets secrets) Option {
	return func(s *Service) {
		s.deployments = deployments
		s.secrets = secrets
	}
}

// WithWebSearch supplies the site's web search credential.
//
// An option because the agent installs and runs without one: left out, the binding
// is the sentinel and the web_search tool reports itself unavailable.
func WithWebSearch(ws webSearch) Option {
	return func(s *Service) {
		s.webSearch = ws
	}
}

// NewService returns a Service. Without WithCluster nothing can be deployed;
// Status still works and reports the blockage.
func NewService(
	repo repository,
	integrations integrations,
	resources resources,
	snapshots snapshots,
	credentials credentials,
	orchestratorURL string,
	opts ...Option,
) *Service {
	s := &Service{
		repo:            repo,
		integrations:    integrations,
		resources:       resources,
		snapshots:       snapshots,
		credentials:     credentials,
		orchestratorURL: orchestratorURL,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Status reports what is installed, what is running, and what — if anything —
// stands in the way.
//
// It never fails for a reason a caller could act on: no cluster, no encryption key
// and no provider key are all *states*, reported in Blocked, because a page that
// 500s cannot tell you to go and configure the thing.
func (s *Service) Status(ctx context.Context) (Status, error) {
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return Status{}, err
	}

	digest, err := agentapp.Digest()
	if err != nil {
		return Status{}, err
	}

	out := Status{
		State:           StateNotInstalled,
		IntegrationID:   cur.IntegrationID,
		DeploymentID:    cur.DeploymentID,
		InternalURL:     cur.InternalURL,
		InstalledTag:    cur.InstalledTag,
		InstalledDigest: cur.InstalledDigest,
		BundleDigest:    digest,
		Tracing:         cur.Tracing,
		AutoFix:         cur.AutoFix,
		MaxIterations:   cur.MaxIterations,
		Blocked:         s.blocked(ctx),
	}
	if cur.IntegrationID == "" {
		return out, nil
	}

	out.State = StateInstalled
	out.UpdateAvailable = cur.InstalledDigest != digest
	if out.UpdateAvailable {
		out.State = StateUpdateAvailable
	}
	out.Edited = s.edited(ctx, cur)

	if cur.DeploymentID == "" {
		return out, nil
	}
	if s.deployments == nil {
		// The row says something is deployed and this orchestrator cannot see the
		// cluster to confirm it. Reporting "deployed" would be a guess.
		return out, nil
	}

	dep, err := s.deployments.Get(ctx, cur.DeploymentID)
	if errors.Is(err, deployment.ErrNotFound) {
		// A deployment removed underneath us is not an error to report — it is the
		// install being back at "not running". So say that completely: the id and the
		// address describe something that is gone, and leaving them set would let a
		// caller offer a roll-out of nothing.
		slog.Warn("agent status: deployment is gone", "deploymentId", cur.DeploymentID)
		out.DeploymentID = ""
		out.InternalURL = ""
		return out, nil
	}
	if err != nil {
		// Anything else is this orchestrator failing to look, not the deployment
		// being absent. Keep reporting what the row says: clearing the id here would
		// offer Deploy against a deployment that is running fine, and pressing it
		// would create a second one.
		slog.Warn("agent status: deployment unreadable", "deploymentId", cur.DeploymentID, "error", err)
		return out, nil
	}
	out.DeploymentStatus = dep.Status
	out.Reason = dep.Detail.Reason
	if dep.Status == "failed" {
		out.State = StateFailed
	} else if !out.UpdateAvailable {
		out.State = StateDeployed
	}
	return out, nil
}

// blocked reports what would stop an install, in the order an operator would fix
// them: no cluster is unfixable from the UI, no encryption key is a chart value, a
// missing provider key is one page away.
func (s *Service) blocked(ctx context.Context) string {
	if s.deployments == nil || s.secrets == nil {
		return BlockedKubernetes
	}
	// Deliberately not Reveal. This runs on every poll of a status page, and
	// decrypting a provider key to ask whether one exists would put the plaintext in
	// memory hundreds of times for a question the metadata already answers.
	settings, err := s.credentials.Get(ctx)
	if err != nil {
		slog.Warn("agent status: cannot read the llm settings", "error", err)
		return BlockedLLMKey
	}
	if !settings.Configured {
		return BlockedLLMKey
	}
	if !s.credentials.EncryptionAvailable() {
		// A key is stored and this orchestrator cannot decrypt it, so the install
		// would fail at the point it reads it.
		return BlockedEncryption
	}
	// He requires the agentic runner: his definition names the standalone octo,
	// dolphin and curl in `cli-run` allow lists, and an allow-list entry is resolved
	// when the flow is BUILT, so on any other image the config does not load at all.
	if !s.deployments.RunnerAvailable(agenticRunner) {
		return BlockedAgenticRunner
	}
	return ""
}

// edited reports whether the installed integration still matches the bundle it was
// installed from.
//
// It compares against the *live* rows rather than the last snapshot, because that is
// what a roll-out would overwrite: snapshotting freezes the working copy, so a
// roll-out publishes whatever is in the integration now.
func (s *Service) edited(ctx context.Context, cur stored) bool {
	if cur.IntegrationID == "" || cur.InstalledDigest == "" {
		return false
	}
	live, err := s.liveDigest(ctx, cur.IntegrationID)
	if err != nil {
		slog.Warn("agent status: cannot read the installed integration", "error", err)
		return false
	}
	return live != cur.InstalledDigest
}

// liveDigest digests the integration's current definition and skill resources the
// same way the bundle is digested, so the two are comparable.
func (s *Service) liveDigest(ctx context.Context, integrationID string) (string, error) {
	it, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		return "", err
	}
	items, err := s.resources.ListByIntegration(ctx, integrationID)
	if err != nil {
		return "", err
	}

	files := map[string]string{"config.yaml": it.Definition}
	for _, item := range items {
		// Only the bundle's own resources count. A template a user added is theirs,
		// and a roll-out does not remove it, so it must not read as an edit.
		if agentapp.IsSkill(item.Name) {
			files[item.Name] = item.Content
		}
	}
	// The same hashing the bundle uses, so the two are comparable. Shared rather
	// than reimplemented: two copies would be correct only while identical, and a
	// change to one would report every installed agent as edited.
	return agentapp.DigestFiles(files), nil
}

// Install creates the agent and deploys it. It is idempotent: an integration that
// already exists is reused, and a tag that already exists is not recreated — so
// pressing Install twice is not an error and does not produce a second agent.
func (s *Service) Install(ctx context.Context, actorID string) (Status, error) {
	if s.deployments == nil || s.secrets == nil {
		return Status{}, ErrClusterUnavailable
	}

	// Two writes, because the integration is created through a different connection
	// than the one holding this row and commits whether or not the rest does.
	// Recording the id first makes that survivable: a later failure rolls back to a
	// row that already knows which integration is ours, and the next attempt reuses
	// it rather than failing on the unique name.
	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		return s.ensureIntegration(ctx, cur, actorID)
	}); err != nil {
		return Status{}, err
	}
	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		return s.install(ctx, cur, actorID)
	}); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// ensureIntegration records which integration is the agent's, creating it the first
// time. Everything after this can fail and be retried.
func (s *Service) ensureIntegration(ctx context.Context, cur stored, actorID string) (stored, error) {
	if cur.IntegrationID != "" {
		_, err := s.integrations.Get(ctx, cur.IntegrationID)
		switch {
		case err == nil:
			return cur, nil
		case !errors.Is(err, integration.ErrNotFound):
			return cur, err
		}
		// The record points at an integration that no longer exists — deleted, or a
		// purge that failed after removing it. Recreating it is what was asked for,
		// and the snapshot and digest go with the id: they describe versions of an
		// integration that is gone, so keeping them would have the next publish
		// reference a row that no longer exists.
		slog.Warn("agent install: the recorded integration is gone, creating it again",
			"integrationId", cur.IntegrationID)
		cur.IntegrationID = ""
		cur.SnapshotID = ""
		cur.InstalledTag = ""
		cur.InstalledDigest = ""
	}
	definition, err := agentapp.Definition()
	if err != nil {
		return cur, err
	}

	it, err := s.integrations.Create(ctx, agentapp.Name, definition, actorID)
	if errors.Is(err, integration.ErrNameTaken) {
		// An earlier attempt created it and did not get as far as recording it — the
		// narrow window where even the two-step write above loses. Adopting it beats
		// refusing to install over an integration the installer itself made.
		//
		// But only if it is recognisably ours: an integration a user happens to have
		// given the same name is theirs, and adopting it would snapshot, deploy and
		// then replace their work. So the definition has to declare the agent's own
		// service name, which nothing but this bundle does.
		adopted, findErr := s.findByName(ctx, agentapp.Name)
		switch {
		case findErr != nil:
			return cur, err
		case !agentapp.IsAgentDefinition(adopted.Definition):
			return cur, err
		}
		it, err = adopted, nil
		slog.Warn("agent install adopted an integration left by an earlier attempt",
			"integrationId", it.ID, "name", agentapp.Name)
	}
	if err != nil {
		return cur, err
	}

	next := cur
	next.IntegrationID = it.ID
	// Tracing on from the first deploy: the usual default is off for throughput, and
	// an agent answering a handful of questions has none to lose, while "what did he
	// do, and was he told to" is worth being able to answer about every run.
	//
	// Only on the first install. Turning it off afterwards is a decision, and a later
	// redeploy must not quietly undo it.
	next.Tracing = true
	next.InstalledAt = time.Now().UTC()
	next.UpdatedAt = next.InstalledAt
	return next, nil
}

// findByName resolves an integration by name, case-insensitively as the unique
// index does. Over the full list because a by-name read is not on the service and
// one recovery path does not justify adding it.
func (s *Service) findByName(ctx context.Context, name string) (integration.Integration, error) {
	items, err := s.integrations.List(ctx)
	if err != nil {
		return integration.Integration{}, err
	}
	for _, it := range items {
		if strings.EqualFold(it.Name, name) {
			return it, nil
		}
	}
	return integration.Integration{}, integration.ErrNotFound
}

// install is the body of Install after the integration exists, run with the
// settings row locked.
func (s *Service) install(ctx context.Context, cur stored, actorID string) (stored, error) {
	if cur.IntegrationID == "" {
		return cur, ErrNotInstalled
	}
	digest, err := agentapp.Digest()
	if err != nil {
		return cur, err
	}

	// Each step names itself in its error: an install is half a dozen operations
	// against the database, the cluster's Secret and the Kubernetes API, and only the
	// code taking a step knows which one failed.
	next := cur
	if err := s.syncResources(ctx, next.IntegrationID, actorID); err != nil {
		return cur, fmt.Errorf("write the agent's skills as resources: %w", err)
	}

	snap, err := s.publishBundle(ctx, cur, digest)
	if err != nil {
		return cur, fmt.Errorf("publish the agent as a version: %w", err)
	}
	next.SnapshotID = snap.ID
	next.InstalledTag = snap.Tag
	next.InstalledDigest = digest

	bindings, err := s.envBindings(ctx, next)
	if err != nil {
		return cur, err
	}

	// He is deployed able to do the job he is installed for.
	//
	// While somebody is chatting, his tools spend that person's token and these
	// grants change nothing. They are for the unattended path, where his own token is
	// the only credential the run has: platform:runtime is in no rule on either API,
	// so without them every call an unattended triage makes is refused, reads
	// included. An administrator can take the grants back on the deployment.
	dep, err := s.deployments.Deploy(ctx, next.IntegrationID, deployment.Settings{
		Replicas:   1,
		SnapshotID: snap.ID,
		Tracing:    next.Tracing,
		Env:        bindings,
		Access:     []string{deployment.AccessDeveloper, deployment.AccessOperator},
		// The runner he needs, asked for the same way any integration asks; see
		// blocked(), which refuses the install up front when there is no such image.
		Runner: agenticRunner,
	})
	if err != nil {
		return cur, fmt.Errorf("deploy version %q: %w", snap.Tag, err)
	}

	meta := deployment.ParseMetadata(dep.Metadata)
	next.DeploymentID = dep.ID
	next.Slug = meta.Slug
	next.InternalURL = meta.InternalURL
	next.UpdatedAt = time.Now().UTC()

	slog.Info("agent installed",
		"integrationId", next.IntegrationID, "deploymentId", next.DeploymentID,
		"tag", next.InstalledTag, "internalUrl", next.InternalURL)
	return next, nil
}

// syncResources writes the bundle's skills onto the integration, updating a
// resource that already exists rather than failing on its name. actorID is the
// user whose action caused the write, so the skill files carry the same
// attribution as the definition written beside them.
func (s *Service) syncResources(ctx context.Context, integrationID, actorID string) error {
	skills, err := agentapp.Skills()
	if err != nil {
		return err
	}
	existing, err := s.resources.ListByIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	byName := make(map[string]resource.Resource, len(existing))
	for _, item := range existing {
		byName[item.Name] = item
	}

	// Sorted, so an install writes its resources in a deterministic order and a
	// failure halfway through leaves the same partial state every time.
	names := make([]string, 0, len(skills))
	for name := range skills {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		content := skills[name]
		if current, ok := byName[name]; ok {
			if current.Content == content && current.Kind == agentapp.SkillResourceKind {
				continue
			}
			if _, err := s.resources.Update(ctx, integrationID, current.ID,
				agentapp.SkillResourceKind, name, content, actorID); err != nil {
				return err
			}
			continue
		}
		if _, err := s.resources.Create(ctx, integrationID,
			agentapp.SkillResourceKind, name, content, actorID); err != nil {
			return err
		}
	}

	// A skill the bundle stopped shipping has to go, or liveDigest keeps counting it
	// (it still matches the skills/ prefix) while the bundle digest does not, and the
	// agent reports as permanently edited. Only the bundle's own resources are
	// considered — a template somebody else added is theirs.
	for name, item := range byName {
		if _, shipped := skills[name]; shipped || !agentapp.IsSkill(name) {
			continue
		}
		if err := s.resources.Delete(ctx, integrationID, item.ID); err != nil {
			return err
		}
		slog.Info("agent skill removed", "integrationId", integrationID, "resource", name)
	}
	return nil
}

// ensureTag returns the version tag for this bundle, creating it only if it is not
// already there. Tags are immutable, so a bundle that has been installed before
// already has its tag and creating it again would fail.
// publishBundle publishes the embedded bundle as a version of the integration and
// returns the snapshot to deploy.
//
// The tag names the octo release rather than the bundle's contents, which is what
// makes it worth reading — but the two only move together in a released binary. A
// build made between releases can carry a changed bundle under a version that is
// already published, and reusing that tag would deploy the older snapshot while the
// row recorded the newer digest. So the release's tag is used only when this bundle
// is provably the one behind it, and anything else is published beside it under a
// build tag.
//
// Reuse is decided on CONTENT rather than on the settings row: a snapshot of exactly
// the definition about to be published *is* that publication, whatever it is tagged;
// anything else gets a new one. The row records the digest of the BUNDLE while what
// is published is a snapshot of the LIVE definition, and on an install that adopted
// an existing integration the two are different documents.
func (s *Service) publishBundle(ctx context.Context, cur stored, digest string) (snapshot.Snapshot, error) {
	// What Create will actually freeze: the integration as it stands now. A roll-out
	// has just republished the bundle over it, so this is the bundle; an install has
	// not, so it may be someone's edits — and either way it is what the deployment
	// would run.
	it, err := s.integrations.Get(ctx, cur.IntegrationID)
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	existing, err := s.snapshots.ListByIntegration(ctx, cur.IntegrationID)
	if err != nil {
		return snapshot.Snapshot{}, err
	}

	taken := make(map[string]bool, len(existing))
	for _, snap := range existing {
		if snap.Definition == it.Definition {
			return snap, nil
		}
		taken[snap.Tag] = true
	}

	// Nothing published holds this definition, so it needs a version of its own under
	// the first free tag: the release's tag, then one derived from the digest, then a
	// suffix for the unexpected case where both names are held by other content.
	//
	// ErrTagExists is tolerated rather than returned, because the check above and the
	// write below are not one atomic step — a version cut by hand in between takes a
	// tag this loop just read as free, and skipping to the next candidate turns a
	// lost race into a version with a different name.
	for _, tag := range candidateTags(digest) {
		if taken[tag] {
			continue
		}
		snap, err := s.snapshots.Create(ctx, cur.IntegrationID, tag)
		if errors.Is(err, snapshot.ErrTagExists) {
			continue
		}
		return snap, err
	}
	return snapshot.Snapshot{}, fmt.Errorf(
		"publish the agent: every candidate tag for %s is held by different content", agentapp.Tag())
}

// candidateTags is the order tags are tried in when publishing a bundle that no
// existing version holds.
func candidateTags(digest string) []string {
	tags := []string{agentapp.Tag(), agentapp.BuildTag(digest)}
	for n := 2; n <= 9; n++ {
		tags = append(tags, fmt.Sprintf("%s-%d", agentapp.BuildTag(digest), n))
	}
	return tags
}

func (s *Service) ensureTag(ctx context.Context, integrationID, tag string) (snapshot.Snapshot, error) {
	existing, err := s.snapshots.ListByIntegration(ctx, integrationID)
	if err != nil {
		return snapshot.Snapshot{}, err
	}
	for _, snap := range existing {
		if snap.Tag == tag {
			return snap, nil
		}
	}
	return s.snapshots.Create(ctx, integrationID, tag)
}

// envBindings resolves the site's LLM settings, and the operator's own settings,
// into the deployment's environment.
//
// The key goes to a cluster secret and the binding carries only its name, so the
// key never enters the deployment record — the same contract every other secret
// binding has.
func (s *Service) envBindings(ctx context.Context, cur stored) (map[string]deployment.EnvBinding, error) {
	creds, err := s.credentials.Reveal(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the site's llm settings: %w", err)
	}
	connectorType, ok := ConnectorTypeFor(creds.Provider)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, creds.Provider)
	}
	// An empty URL would satisfy the deploy-time check for required variables and
	// then fail on the agent's first call back — the late failure this binding
	// exists to prevent. Refusing here says so while someone is looking.
	if s.orchestratorURL == "" {
		return nil, ErrNoOrchestratorURL
	}
	// The provider key becomes an ordinary platform secret, because the agent is
	// deployed through the ordinary path and that is how it carries a credential:
	// the deployment binds LLM_API_KEY to this name, so the key itself never enters
	// a deployment record.
	if _, err := s.secrets.Create(ctx, llmKeySecret, creds.APIKey); err != nil {
		return nil, fmt.Errorf("store the provider key as platform secret %s: %w", llmKeySecret, err)
	}

	bindings := map[string]deployment.EnvBinding{
		envConnectorType: {Value: connectorType},
		envModel:         {Value: creds.Model},
		envAPIKey:        {Secret: llmKeySecret},
		envOrchestrator:  {Value: s.orchestratorURL},
	}

	// The web search key, when there is one. Always bound, because the connector
	// that reads it starts eagerly and refuses an empty value — see
	// WebSearchUnconfigured. So the choice here is a secret reference or the
	// sentinel, never nothing.
	webSearchKey, err := s.webSearchKey(ctx)
	if err != nil {
		return nil, err
	}
	bindings[envWebSearchKey] = webSearchKey

	// Bound only when an operator set one. Left out, the definition's own default
	// applies — which keeps the number in one place, and means raising the shipped
	// default reaches every installation that never touched this.
	if cur.MaxIterations > 0 {
		bindings[envMaxIterations] = deployment.EnvBinding{Value: strconv.Itoa(cur.MaxIterations)}
	}
	// Bound only when it is on, on the same terms: the definition ships with it
	// off, so leaving it out is the same as saying false — and an installation
	// that never touched this keeps whatever the definition decides is safe.
	if cur.AutoFix {
		bindings[envAutoFix] = deployment.EnvBinding{Value: "true"}
	}
	return bindings, nil
}

// webSearchKey resolves the binding for PARALLEL_API_KEY: a reference to the
// cluster secret the key was just written to, or the sentinel that tells the agent
// his web_search tool has nothing behind it.
//
// A key that cannot be read is *not* fatal. Refusing the whole install because an
// optional credential could not be decrypted would trade a missing tool for a
// missing agent; the sentinel says the same thing the unconfigured case says, and
// the error is logged where an operator can find it.
func (s *Service) webSearchKey(ctx context.Context) (deployment.EnvBinding, error) {
	unconfigured := deployment.EnvBinding{Value: WebSearchUnconfigured}
	if s.webSearch == nil {
		return unconfigured, nil
	}
	key, err := s.webSearch.Reveal(ctx)
	if err != nil {
		// Deliberately not the removal path below: a key that could not be read is
		// not a key that was removed, and deleting the secret on the strength of a
		// decryption failure would destroy the credential the next roll-out needs.
		slog.Error("could not read the site's web search key; the agent installs without it",
			"error", err)
		return unconfigured, nil
	}
	if key == "" {
		// Nothing is stored, so nothing should be left in the cluster either: a key
		// configured and later removed would otherwise survive as a live credential
		// with nothing owning it.
		//
		// force, because the deployment about to be replaced still references it. Not
		// found is the ordinary case: most installations never stored one.
		if err := s.secrets.Delete(ctx, webSearchKeySecret, true); err != nil &&
			!errors.Is(err, secret.ErrNotFound) {
			// Not fatal: the binding below is the sentinel either way, so what is left
			// is a stale secret rather than a working credential.
			slog.Error("could not remove the stored web search key secret",
				"secret", webSearchKeySecret, "error", err)
		}
		return unconfigured, nil
	}
	if _, err := s.secrets.Create(ctx, webSearchKeySecret, key); err != nil {
		return deployment.EnvBinding{}, fmt.Errorf(
			"store the web search key as platform secret %s: %w", webSearchKeySecret, err)
	}
	return deployment.EnvBinding{Secret: webSearchKeySecret}, nil
}

// Rollout publishes the bundle this binary carries and rolls the deployment onto
// it.
//
// Snapshotting freezes the integration's *working copy*, so this publishes whatever
// is in the integration now: a roll-out replaces local edits with the shipped agent,
// which is what Status reports as Edited beforehand.
func (s *Service) Rollout(ctx context.Context, actorID string) (Status, error) {
	if s.deployments == nil || s.secrets == nil {
		return Status{}, ErrClusterUnavailable
	}

	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		if cur.IntegrationID == "" {
			return cur, ErrNotInstalled
		}
		running, err := s.hasRunningDeployment(ctx, cur)
		if err != nil {
			return cur, err
		}
		if !running {
			// Nothing is running, so there is no rolling update to do — but the
			// definition still has to be republished, or asking for the shipped agent
			// would freeze the local edits into the new tag instead. Install alone does
			// not republish: it must not overwrite a definition on a plain retry.
			//
			// The recorded deployment may be gone rather than absent, and a fresh
			// deploy is the right answer to both, so the two cases are one.
			if err := s.republish(ctx, cur.IntegrationID, actorID); err != nil {
				return cur, err
			}
			return s.install(ctx, cur, actorID)
		}
		return s.rollout(ctx, cur, actorID)
	}); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// hasRunningDeployment reports whether the row names a deployment that still
// exists, so Rollout can tell a rolling update from a fresh deploy.
func (s *Service) hasRunningDeployment(ctx context.Context, cur stored) (bool, error) {
	if cur.DeploymentID == "" {
		return false, nil
	}
	return s.deploymentExists(ctx, cur.DeploymentID)
}

// preserveEdits tags the integration's live definition when it differs from the
// bundle about to replace it, so the edits survive the roll-out as a version anyone
// can read, deploy or copy back.
//
// Compared against the bundle rather than against the installed digest: what matters
// is whether republishing is about to change anything. When they match no tag is
// created, so a plain redeploy does not mint a version per press.
//
// A failure here fails the roll-out, because continuing past a failed snapshot would
// discard exactly the edits this exists to keep.
func (s *Service) preserveEdits(ctx context.Context, integrationID, bundleDigest string) error {
	live, err := s.liveDigest(ctx, integrationID)
	if err != nil {
		return fmt.Errorf("read the integration before replacing it: %w", err)
	}
	if live == bundleDigest {
		return nil
	}
	snap, err := s.ensureTag(ctx, integrationID, agentapp.EditedTag(live))
	if err != nil {
		return fmt.Errorf("freeze the current definition before replacing it: %w", err)
	}
	slog.Info("agent edits preserved before roll-out", "tag", snap.Tag)
	return nil
}

// deploymentExists reports whether the recorded deployment is still there.
//
// Only ErrNotFound counts as gone; every other failure is propagated. The answer
// chooses between rolling the deployment out and creating a new one, so answering
// "not there" to a timeout would build a second agent beside a healthy first.
func (s *Service) deploymentExists(ctx context.Context, deploymentID string) (bool, error) {
	switch _, err := s.deployments.Get(ctx, deploymentID); {
	case errors.Is(err, deployment.ErrNotFound):
		slog.Warn("agent rollout: recorded deployment is gone, deploying a new one",
			"deploymentId", deploymentID)
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read the recorded deployment: %w", err)
	default:
		return true, nil
	}
}

// republish writes the bundle's definition and skills over the integration. This is
// what "rolling out replaces local edits" means, and it is only ever called from a
// roll-out — an install must not overwrite a definition on a retry.
func (s *Service) republish(ctx context.Context, integrationID, actorID string) error {
	definition, err := agentapp.Definition()
	if err != nil {
		return err
	}
	if _, err := s.integrations.Update(ctx, integrationID, agentapp.Name, definition, actorID); err != nil {
		return err
	}
	return s.syncResources(ctx, integrationID, actorID)
}

// rollout is the body of Rollout, run with the settings row locked.
func (s *Service) rollout(ctx context.Context, cur stored, actorID string) (stored, error) {
	digest, err := agentapp.Digest()
	if err != nil {
		return cur, err
	}

	// Freeze the live definition before republishing writes over it, so a roll-out
	// that discards someone's edits leaves them recoverable as a version rather than
	// gone. Snapshotting is the only place edits could survive: republish overwrites
	// the working copy, and the tag published below is of the bundle, not of what was
	// there before it.
	if err := s.preserveEdits(ctx, cur.IntegrationID, digest); err != nil {
		return cur, err
	}

	if err := s.republish(ctx, cur.IntegrationID, actorID); err != nil {
		return cur, err
	}

	snap, err := s.publishBundle(ctx, cur, digest)
	if err != nil {
		return cur, err
	}

	bindings, err := s.envBindings(ctx, cur)
	if err != nil {
		return cur, err
	}

	// The env is re-resolved and passed explicitly, so a roll-out also picks up a
	// provider or model changed in the LLM settings since the install. Tracing is
	// nil: it is the deployment's own setting and SetTracing is what changes it. The
	// runner is stated rather than inherited, because a deployment whose row names
	// none would otherwise be preserved onto an image the bundle cannot run on.
	runner := agenticRunner
	dep, err := s.deployments.Rollout(ctx, cur.DeploymentID, snap.ID, bindings, nil, &runner)
	if err != nil {
		return cur, err
	}

	next := cur
	meta := deployment.ParseMetadata(dep.Metadata)
	next.SnapshotID = snap.ID
	next.InstalledTag = snap.Tag
	next.InstalledDigest = digest
	if meta.InternalURL != "" {
		next.InternalURL = meta.InternalURL
	}
	next.UpdatedAt = time.Now().UTC()

	slog.Info("agent rolled out", "deploymentId", next.DeploymentID, "tag", next.InstalledTag)
	return next, nil
}

// SetTracing turns tracing on or off for the agent's pods.
//
// It is a roll-out because OCTO_TRACING is read at pod startup, so the setting only
// reaches it by replacing the pods. Nothing else changes: same tag, same env.
func (s *Service) SetTracing(ctx context.Context, on bool) (Status, error) {
	if s.deployments == nil {
		return Status{}, ErrClusterUnavailable
	}

	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		if cur.IntegrationID == "" {
			return cur, ErrNotInstalled
		}
		if cur.DeploymentID == "" {
			return cur, ErrNotDeployed
		}
		if _, err := s.deployments.Rollout(ctx, cur.DeploymentID, cur.SnapshotID, nil, &on, nil); err != nil {
			return cur, err
		}
		next := cur
		next.Tracing = on
		next.UpdatedAt = time.Now().UTC()
		return next, nil
	}); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// SetMaxIterations overrides how many tool-calling turns one of the agent's runs
// may take, or clears the override with zero.
//
// It is a roll-out for the same reason SetTracing is: the value is an environment
// variable read at pod startup. Unlike SetTracing it travels in the env bindings,
// which is why this passes them and the tracing toggle passes nil.
//
// Zero is not "no turns" but "no override", and is the only way back to the
// definition's own default once one has been set.
func (s *Service) SetMaxIterations(ctx context.Context, iterations int) (Status, error) {
	if s.deployments == nil {
		return Status{}, ErrClusterUnavailable
	}
	if iterations != 0 && (iterations < MinIterations || iterations > MaxIterationsCeiling) {
		return Status{}, fmt.Errorf("%w: %d is outside %d..%d",
			ErrInvalidIterations, iterations, MinIterations, MaxIterationsCeiling)
	}

	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		if cur.IntegrationID == "" {
			return cur, ErrNotInstalled
		}
		if cur.DeploymentID == "" {
			return cur, ErrNotDeployed
		}

		next := cur
		next.MaxIterations = iterations

		// Resolved from next, so the roll-out carries the new value. Clearing an
		// override drops the binding entirely rather than storing a zero that means
		// nothing.
		bindings, err := s.envBindings(ctx, next)
		if err != nil {
			return cur, err
		}
		// The runner is stated for the same reason Rollout states it: a row that names
		// none would otherwise be preserved onto an image the flow cannot load from.
		runner := agenticRunner
		if _, err := s.deployments.Rollout(ctx, cur.DeploymentID, cur.SnapshotID, bindings, nil, &runner); err != nil {
			return cur, err
		}

		next.UpdatedAt = time.Now().UTC()
		return next, nil
	}); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// SetDeploymentSettings applies the settings that live on the agent's pods, in
// one rollout.
//
// Both are environment variables read at pod startup, so each alone has to replace
// the pods — applying them together is what keeps one save from rolling the agent
// twice.
//
// Nil means "leave this one alone", which lets a caller send only what changed. Both
// nil is a no-op rather than a pointless rollout.
func (s *Service) SetDeploymentSettings(
	ctx context.Context, iterations *int, autoFix *bool,
) (Status, error) {
	if s.deployments == nil {
		return Status{}, ErrClusterUnavailable
	}
	if iterations == nil && autoFix == nil {
		return s.Status(ctx)
	}
	if iterations != nil && *iterations != 0 &&
		(*iterations < MinIterations || *iterations > MaxIterationsCeiling) {
		return Status{}, fmt.Errorf("%w: %d is outside %d..%d",
			ErrInvalidIterations, *iterations, MinIterations, MaxIterationsCeiling)
	}

	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		if cur.IntegrationID == "" {
			return cur, ErrNotInstalled
		}
		if cur.DeploymentID == "" {
			return cur, ErrNotDeployed
		}

		next := cur
		if iterations != nil {
			next.MaxIterations = *iterations
		}
		if autoFix != nil {
			next.AutoFix = *autoFix
		}

		bindings, err := s.envBindings(ctx, next)
		if err != nil {
			return cur, err
		}
		runner := agenticRunner
		if _, err := s.deployments.Rollout(ctx, cur.DeploymentID, cur.SnapshotID, bindings, nil, &runner); err != nil {
			return cur, err
		}

		next.UpdatedAt = time.Now().UTC()
		return next, nil
	}); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// SetAutoFix decides whether the troubleshooter may change this installation, or
// may only look at it and report.
//
// A roll-out for the same reason SetMaxIterations is: the value is an environment
// variable read at pod startup, and it travels in the env bindings.
//
// It is its own call rather than a field on a general update because turning it on
// changes what an unattended alert may do to a live installation — from sending an
// email to rolling out a definition — which is worth its own audit point.
func (s *Service) SetAutoFix(ctx context.Context, on bool) (Status, error) {
	if s.deployments == nil {
		return Status{}, ErrClusterUnavailable
	}

	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		if cur.IntegrationID == "" {
			return cur, ErrNotInstalled
		}
		if cur.DeploymentID == "" {
			return cur, ErrNotDeployed
		}

		next := cur
		next.AutoFix = on

		bindings, err := s.envBindings(ctx, next)
		if err != nil {
			return cur, err
		}
		runner := agenticRunner
		if _, err := s.deployments.Rollout(ctx, cur.DeploymentID, cur.SnapshotID, bindings, nil, &runner); err != nil {
			return cur, err
		}

		next.UpdatedAt = time.Now().UTC()
		return next, nil
	}); err != nil {
		return Status{}, err
	}
	return s.Status(ctx)
}

// Repair clears a stored deployment id whose deployment no longer exists, and
// reports whether it had to.
//
// Status masks this case in what it returns but never writes the correction back,
// so without this the pointer outlives the deployment rows the reconciler deletes.
//
// The refusals are the interesting half: a missing id is nothing to repair, and an
// unreadable deployment is a failure to look rather than an absent deployment —
// clearing on that would offer a deploy against an agent that is running fine.
func (s *Service) Repair(ctx context.Context) (bool, error) {
	if s.deployments == nil {
		return false, nil
	}

	repaired := false
	err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		repaired = false
		if cur.DeploymentID == "" {
			return cur, nil
		}
		_, err := s.deployments.Get(ctx, cur.DeploymentID)
		if err == nil {
			return cur, nil
		}
		if !errors.Is(err, deployment.ErrNotFound) {
			return cur, nil
		}

		slog.Warn("agent repair: forgetting a deployment that no longer exists",
			"deploymentId", cur.DeploymentID)
		next := cur
		next.DeploymentID = ""
		next.InternalURL = ""
		next.UpdatedAt = time.Now().UTC()
		repaired = true
		return next, nil
	})
	if err != nil {
		return false, err
	}
	return repaired, nil
}

// Uninstall removes the deployment, and with purge the integration too.
//
// The default keeps the integration: it holds the agent's definition, which a user
// may have edited, and undeploying is the reversible half. purge is the one that
// throws the work away.
func (s *Service) Uninstall(ctx context.Context, purge bool) error {
	return s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		if cur.IntegrationID == "" {
			return cur, ErrNotInstalled
		}
		next := cur

		if cur.DeploymentID != "" {
			if s.deployments == nil {
				return cur, ErrClusterUnavailable
			}
			if err := s.deployments.Undeploy(ctx, cur.DeploymentID); err != nil {
				return cur, err
			}
			next.DeploymentID = ""
			next.InternalURL = ""
			next.Slug = ""
		}

		if purge {
			// Already gone is the outcome this asks for, not a failure: treating it as
			// one leaves a half-purged install that every retry fails to finish.
			if err := s.integrations.Delete(ctx, cur.IntegrationID); err != nil &&
				!errors.Is(err, integration.ErrNotFound) {
				return cur, err
			}
			// The credential is the part that most needs to go. Left behind it is a
			// plaintext provider key in the cluster with nothing owning it and no
			// record pointing at it. force, because the deployment that referenced it
			// has just been removed and the reference may still be visible.
			if s.secrets != nil {
				for _, name := range []string{llmKeySecret, webSearchKeySecret} {
					// Already gone is the outcome this asks for, and the ordinary case
					// for the web search key, which most installs never configure.
					if err := s.secrets.Delete(ctx, name, true); err != nil &&
						!errors.Is(err, secret.ErrNotFound) {
						// Not fatal: the install is gone either way, and failing here
						// would leave the record describing an agent that no longer
						// exists. Logged loudly because it is a credential.
						slog.Error("agent purge could not remove a key secret",
							"secret", name, "error", err)
					}
				}
			}
			// Everything the record described is gone, so the record goes with it —
			// leaving a digest behind would report an update available for an agent
			// that is not installed.
			next = stored{}
		}
		next.UpdatedAt = time.Now().UTC()

		slog.Info("agent uninstalled", "purged", purge, "integrationId", cur.IntegrationID)
		return next, nil
	})
}

// ConnectorTypeFor maps a site LLM provider to the runtime connector that talks to
// it.
//
// The connector type names are not importable from here, so they are written out.
// A provider with no mapping is refused rather than guessed at.
func ConnectorTypeFor(provider string) (string, bool) {
	switch provider {
	case llm.ProviderAnthropic:
		return "llm-anthropic", true
	case llm.ProviderOpenAI:
		return "llm-openai", true
	case llm.ProviderGoogle:
		return "llm-gemini", true
	case llm.ProviderOpenRouter:
		return "llm-openrouter", true
	default:
		return "", false
	}
}
