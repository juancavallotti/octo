package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
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
		Create(ctx context.Context, integrationID, kind, name, content string) (resource.Resource, error)
		Update(ctx context.Context, integrationID, id, kind, name, content string) (resource.Resource, error)
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
		Rollout(ctx context.Context, id, snapshotID string, env map[string]deployment.EnvBinding, tracing *bool) (deployment.Deployment, error)
		Get(ctx context.Context, id string) (deployment.Deployment, error)
		Undeploy(ctx context.Context, id string) error
	}

	secrets interface {
		Create(ctx context.Context, name, value string) (secret.Secret, error)
		Delete(ctx context.Context, name string, force bool) error
	}

	// credentials is llm.Service. Reveal is the read path that exists for exactly
	// this consumer and is on no route — and it decrypts, so only install and
	// rollout call it. Get and EncryptionAvailable answer "is one configured?"
	// without ever materialising the key, which is what a polled status page needs.
	credentials interface {
		Get(ctx context.Context) (llm.Settings, error)
		EncryptionAvailable() bool
		Reveal(ctx context.Context) (llm.Credentials, error)
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

	// orchestratorURL is bound on the deployment as ORCHESTRATOR_URL. The
	// orchestrator injects the same value into every pod, but the deploy-time check
	// for required variables sees only bindings, so a required declaration with no
	// binding is refused before the pod exists.
	orchestratorURL string
}

// Option configures a Service.
type Option func(*Service)

// WithCluster supplies the half of the service that needs Kubernetes.
//
// An option rather than two more parameters, precisely so that "no cluster" is the
// absence of a call rather than two nils passed in. A nil *deployment.Service handed
// to an interface parameter is a non-nil interface holding a nil pointer, so the
// nil check inside would silently pass and the failure would arrive later as a
// panic — which is exactly the shape of bug the check exists to prevent.
func WithCluster(deployments deployer, secrets secrets) Option {
	return func(s *Service) {
		s.deployments = deployments
		s.secrets = secrets
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
	if err != nil {
		// A deployment that has been removed underneath us is not an error to
		// report — it is the install being back at "not running".
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
	return ""
}

// edited reports whether the installed integration still matches the bundle it was
// installed from.
//
// It compares against the *live* rows rather than the last snapshot, because that
// is what a roll-out would overwrite: snapshotting freezes the working copy, so a
// roll-out necessarily publishes whatever is in the integration now. Someone
// changing the agent is supported and expected — this exists so the roll-out can
// say what it is about to replace.
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

	// Two writes, deliberately, because the integration is created through a
	// different connection than the one holding this row — so it commits whether or
	// not the rest of the install does.
	//
	// Recording the id first makes that survivable. If the deploy then fails on a
	// transient API-server error, the settings roll back to a row that already knows
	// which integration is ours, and the next attempt reuses it. Doing it all under
	// one lock instead would roll the id back and leave an integration nobody owns,
	// after which every install fails on the unique name and the operator is told to
	// rename something the installer itself created.
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
		return cur, nil
	}
	definition, err := agentapp.Definition()
	if err != nil {
		return cur, err
	}

	it, err := s.integrations.Create(ctx, agentapp.Name, definition, actorID)
	if errors.Is(err, integration.ErrNameTaken) {
		// An earlier attempt created it and did not get as far as recording it — the
		// narrow window where even the two-step write above loses. Adopting it beats
		// telling the operator to rename an integration the installer itself made.
		//
		// But only if it is recognisably ours. An integration a user happens to have
		// named "Dr. Octo" is theirs, and adopting it would snapshot and deploy their
		// work and then replace it on the next roll-out — with Edited in the status
		// as the only warning, after the fact. So the definition has to declare the
		// agent's own service name, which nothing but this bundle does.
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

	next := cur
	if err := s.syncResources(ctx, next.IntegrationID); err != nil {
		return cur, err
	}

	snap, err := s.ensureTag(ctx, next.IntegrationID, agentapp.Tag(digest))
	if err != nil {
		return cur, err
	}
	next.SnapshotID = snap.ID
	next.InstalledTag = snap.Tag
	next.InstalledDigest = digest

	bindings, err := s.envBindings(ctx)
	if err != nil {
		return cur, err
	}

	dep, err := s.deployments.Deploy(ctx, next.IntegrationID, deployment.Settings{
		Replicas:   1,
		SnapshotID: snap.ID,
		Tracing:    next.Tracing,
		Env:        bindings,
	})
	if err != nil {
		return cur, err
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
// resource that already exists rather than failing on its name.
func (s *Service) syncResources(ctx context.Context, integrationID string) error {
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
				agentapp.SkillResourceKind, name, content); err != nil {
				return err
			}
			continue
		}
		if _, err := s.resources.Create(ctx, integrationID,
			agentapp.SkillResourceKind, name, content); err != nil {
			return err
		}
	}

	// A skill the bundle no longer ships has to go, or it stays on the integration
	// for ever: liveDigest counts it (it still matches the skills/ prefix) while the
	// bundle digest does not, so the agent would report as permanently edited and no
	// roll-out would clear it. Only the bundle's own resources are considered — a
	// template a user added is theirs.
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

// envBindings resolves the site's LLM settings into the deployment's environment.
//
// The key goes to a cluster secret and the binding carries only its name, so the
// key never enters the deployment record — the same contract every other secret
// binding has.
func (s *Service) envBindings(ctx context.Context) (map[string]deployment.EnvBinding, error) {
	creds, err := s.credentials.Reveal(ctx)
	if err != nil {
		return nil, err
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
	if _, err := s.secrets.Create(ctx, llmKeySecret, creds.APIKey); err != nil {
		return nil, err
	}

	return map[string]deployment.EnvBinding{
		envConnectorType: {Value: connectorType},
		envModel:         {Value: creds.Model},
		envAPIKey:        {Secret: llmKeySecret},
		envOrchestrator:  {Value: s.orchestratorURL},
	}, nil
}

// Rollout publishes the bundle this binary carries and rolls the deployment onto
// it.
//
// Snapshotting freezes the integration's *working copy*, so this necessarily
// publishes whatever is in the integration now — which means a roll-out replaces
// local edits with the shipped agent. That is why Status reports Edited: so the
// choice is made knowingly rather than discovered afterwards.
func (s *Service) Rollout(ctx context.Context, actorID string) (Status, error) {
	if s.deployments == nil || s.secrets == nil {
		return Status{}, ErrClusterUnavailable
	}

	if err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		if cur.IntegrationID == "" {
			return cur, ErrNotInstalled
		}
		if cur.DeploymentID == "" {
			// Nothing is running, so there is no rolling update to do — but the
			// definition still has to be republished, or an operator who asked for
			// the shipped agent would get their own edits frozen into the new tag.
			// Install alone does not do that, deliberately: it must not overwrite a
			// definition on a plain retry.
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
	return s.syncResources(ctx, integrationID)
}

// rollout is the body of Rollout, run with the settings row locked.
func (s *Service) rollout(ctx context.Context, cur stored, actorID string) (stored, error) {
	digest, err := agentapp.Digest()
	if err != nil {
		return cur, err
	}

	if err := s.republish(ctx, cur.IntegrationID, actorID); err != nil {
		return cur, err
	}

	snap, err := s.ensureTag(ctx, cur.IntegrationID, agentapp.Tag(digest))
	if err != nil {
		return cur, err
	}

	bindings, err := s.envBindings(ctx)
	if err != nil {
		return cur, err
	}

	// The env is re-resolved and passed explicitly, so a roll-out also picks up a
	// provider or model changed in the LLM settings since the install. Tracing is
	// nil: it is the deployment's own setting and SetTracing is what changes it.
	dep, err := s.deployments.Rollout(ctx, cur.DeploymentID, snap.ID, bindings, nil)
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

// SetTracing turns the runtime's tracer on or off for the agent's pods.
//
// It is a roll-out because the runtime reads OCTO_TRACING when it starts, so the
// setting only reaches it by replacing the pods. Nothing else changes: the same
// tag, the same env.
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
		if _, err := s.deployments.Rollout(ctx, cur.DeploymentID, cur.SnapshotID, nil, &on); err != nil {
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
			if err := s.integrations.Delete(ctx, cur.IntegrationID); err != nil {
				return cur, err
			}
			// The credential is the part that most needs to go. Left behind it is a
			// plaintext provider key in the cluster with nothing owning it and no
			// record pointing at it. force, because the deployment that referenced it
			// has just been removed and the reference may still be visible.
			if s.secrets != nil {
				if err := s.secrets.Delete(ctx, llmKeySecret, true); err != nil {
					// Not fatal: the install is gone either way, and failing here
					// would leave the record describing an agent that no longer
					// exists. Logged loudly because it is a credential.
					slog.Error("agent purge could not remove the provider key secret",
						"secret", llmKeySecret, "error", err)
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
// The provider names mirror core.Provider* in runtime/core/llm.go and the connector
// types mirror the connector packages, neither of which the orchestrator can import
// — the runtime is a separate Go module. Taking on that dependency to share six
// strings would be the wrong trade; if a fourth provider appears there, it is added
// here too, and an unmapped one is refused rather than guessed at.
func ConnectorTypeFor(provider string) (string, bool) {
	switch provider {
	case llm.ProviderAnthropic:
		return "llm-anthropic", true
	case llm.ProviderOpenAI:
		return "llm-openai", true
	case llm.ProviderGoogle:
		return "llm-gemini", true
	default:
		return "", false
	}
}
