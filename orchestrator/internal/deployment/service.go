package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake; *Repo
// satisfies it structurally.
type repository interface {
	Create(ctx context.Context, integrationID, status string, settings, metadata json.RawMessage) (Deployment, error)
	Get(ctx context.Context, id string) (Deployment, error)
	ListByIntegration(ctx context.Context, integrationID string) ([]Deployment, error)
	IntegrationIDBySlug(ctx context.Context, slug string) (string, bool, error)
	IntegrationIDBySubdomain(ctx context.Context, subdomain string) (string, bool, error)
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateSettings(ctx context.Context, id string, settings json.RawMessage) error
	UpdateMetadata(ctx context.Context, id string, metadata json.RawMessage) error
	UpdateMetadataAndSettings(ctx context.Context, id string, metadata, settings json.RawMessage) error
	Delete(ctx context.Context, id string) error
}

// integrationStore is the slice of the integration repository the service needs
// to fetch the integration name (and, without a snapshot store, the definition).
// *integration.Repo satisfies it.
type integrationStore interface {
	Get(ctx context.Context, id string) (integration.Integration, error)
}

// snapshotStore reads a version tag's frozen definition and the resources frozen
// alongside it. *snapshot.Service satisfies it. When wired (WithSnapshots),
// deploys must reference a snapshot and ship its frozen definition instead of the
// live one. ListResources lets the deploy check required env vars against the
// tag's frozen .env resources (not just the deploy-time bindings).
type snapshotStore interface {
	Get(ctx context.Context, id string) (snapshot.Snapshot, error)
	ListResources(ctx context.Context, snapshotID string) ([]snapshot.Resource, error)
}

// resourceStore reads an integration's live (working-copy) resources. Wired via
// WithResources, it lets DeployOptions report which env vars the working copy's
// .env files already supply for the Current case (a deploy from Current tags the
// working copy first, so its .env keys are exactly the live ones).
// *resource.Service satisfies it.
type resourceStore interface {
	ListByIntegration(ctx context.Context, integrationID string) ([]resource.Resource, error)
}

// kubeClient is the Kubernetes surface the service drives. *kube.Client
// satisfies it.
type kubeClient interface {
	Apply(ctx context.Context, spec kube.Spec) error
	Rollout(ctx context.Context, spec kube.Spec) error
	Status(ctx context.Context, deploymentID string) (kube.Status, error)
	PodLogs(ctx context.Context, podName, container string, follow bool, tail int64) (io.ReadCloser, error)
	Scale(ctx context.Context, deploymentID string, replicas int32) error
	Delete(ctx context.Context, deploymentID string) error
	InternalURL(slug string, port int) string
	DeleteInternalService(ctx context.Context, slug string) error
	ExternalEnabled() bool
	RunnerEnabled(runner kube.Runner) bool
	ExternalURL(subdomain string) string
	SecretKeyExists(ctx context.Context, name string) (bool, error)
}

// storeCleaner removes a deployment's entries from a deployment-scoped store
// (the KV store and the secret store each implement it), wired via WithStoreCleaner.
type storeCleaner interface {
	DeleteByDeployment(ctx context.Context, deploymentID string) error
}

// Service holds deployment lifecycle logic: it persists deployment rows and
// reconciles them with cluster resources via the kube client.
type Service struct {
	repo         repository
	integrations integrationStore
	snapshots    snapshotStore
	resources    resourceStore
	kube         kubeClient
	cleaners     []storeCleaner
}

// Option customizes a Service at construction.
type Option func(*Service)

// WithStoreCleaner adds a store cleaner so Undeploy drops a deployment's entries
// from that store (best-effort). It may be called more than once to clean several
// stores (e.g. the KV store and the secret store).
func WithStoreCleaner(c storeCleaner) Option {
	return func(s *Service) { s.cleaners = append(s.cleaners, c) }
}

// WithSnapshots wires the snapshot store, switching the service into tagged-deploy
// mode: every deploy must reference a snapshot (version tag) and ships its frozen
// definition. Without it, deploys fall back to the integration's live definition
// (used only in tests; the production wiring always supplies a store).
func WithSnapshots(s snapshotStore) Option {
	return func(svc *Service) { svc.snapshots = s }
}

// WithResources wires the live-resource store so DeployOptions can report the env
// vars an integration's working-copy .env files supply (the Current deploy case).
// Without it, those keys are simply unknown up front — the deploy-time check
// against the frozen snapshot still applies.
func WithResources(r resourceStore) Option {
	return func(svc *Service) { svc.resources = r }
}

// NewService returns a Service. kube may be nil, in which case all operations
// return ErrUnavailable (the caller should not register the routes then).
func NewService(repo repository, integrations integrationStore, kube kubeClient, opts ...Option) *Service {
	s := &Service{repo: repo, integrations: integrations, kube: kube}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// resolveRunner turns a settings value into the runner the workload will use,
// refusing both a name that does not exist and one this installation cannot
// serve.
//
// The two are separate errors because they are separate mistakes with separate
// fixes: an unknown name is a typo in the request, and an unconfigured runner is
// a chart that does not carry the image. Collapsing them into one message would
// send whoever hit it to the wrong place.
func (s *Service) resolveRunner(name string) (kube.Runner, error) {
	runner, err := kube.ParseRunner(name)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrInvalidRunner, name)
	}
	if !s.kube.RunnerEnabled(runner) {
		return "", fmt.Errorf("%w: %q", ErrRunnerUnavailable, runner)
	}
	return runner, nil
}

// RunnerAvailable reports whether a deployment could ask for this runner and be
// served. It takes the settings spelling — the string a Settings carries — so a
// caller deciding whether to offer the choice asks the same question in the same
// words the deploy will be made in. An unknown name is not available, which is
// the honest answer to "could I deploy this".
//
// It exists for the callers that need to say so *before* deploying: the agent
// installer refuses an install it knows would crash-loop, and the deploy form can
// grey out a runner this installation does not carry.
func (s *Service) RunnerAvailable(name string) bool {
	if s.kube == nil {
		return false
	}
	runner, err := kube.ParseRunner(name)
	if err != nil {
		return false
	}
	return s.kube.RunnerEnabled(runner)
}

// Deploy creates a deployment of integrationID with the given settings: it
// records the row to mint an id, then creates the cluster resources
// named/labelled from that id. On a Kubernetes failure it tears down any partial
// resources and removes the row so a failed deploy leaves nothing behind.
func (s *Service) Deploy(ctx context.Context, integrationID string, settings Settings) (Deployment, error) {
	if s.kube == nil {
		return Deployment{}, ErrUnavailable
	}
	it, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		if errors.Is(err, integration.ErrNotFound) {
			return Deployment{}, ErrIntegrationNotFound
		}
		return Deployment{}, err
	}

	// Resolve which definition to deploy. With a snapshot store wired (production),
	// a version tag is required and its frozen definition is shipped; the resolved
	// tag is recorded in metadata. Without one (tests), the live definition is used.
	definition := it.Definition
	var snapID, snapTag string
	if s.snapshots != nil {
		snap, err := s.resolveSnapshot(ctx, integrationID, settings.SnapshotID)
		if err != nil {
			return Deployment{}, err
		}
		definition, snapID, snapTag = snap.Definition, snap.ID, snap.Tag
	}

	replicas := settings.Replicas
	if replicas < 1 {
		replicas = 1
	}

	// The runtime port (and the env the orchestrator supplies to bind it) come from
	// the deployed definition's HTTP_PORT declaration. Only a definition that
	// declares one has an HTTP source listening on a port — those are "networked"
	// and get a Service, a unique internal slug/URL and the option of external
	// exposure. Anything else (a timer, a scheduled job) runs as a bare workload.
	port, runtimeEnv, networked := resolveRuntimeEnv(definition)

	// Resolve the per-deployment env bindings into the two maps the kube spec needs:
	// literal values (the orchestrator-supplied HTTP_PORT/HTTP_HOST stay authoritative
	// and cannot be overridden) and secret references (env var name → cluster-secret
	// key). Referenced secrets are validated to exist before anything is persisted, so
	// a dangling reference fails the deploy cleanly rather than leaving a broken pod.
	literalEnv, secretEnv, err := s.resolveEnvBindings(ctx, runtimeEnv, settings.Env)
	if err != nil {
		return Deployment{}, err
	}

	// Reject a deploy whose definition declares a required env var that nothing
	// provides — neither a deploy-time binding nor a frozen .env resource. A
	// declared default does not satisfy a required var (matching the runtime), so
	// this catches the "new version needs a new required var" gap up front, with a
	// clear message, instead of leaving the pod to crash-loop on load.
	provided := providedEnvKeys(settings.Env)
	fileKeys, err := s.snapshotEnvFileKeys(ctx, snapID)
	if err != nil {
		return Deployment{}, err
	}
	for k := range fileKeys {
		provided[k] = struct{}{}
	}
	if missing := missingRequiredEnv(definition, provided); len(missing) > 0 {
		return Deployment{}, fmt.Errorf("%w: %s", ErrMissingRequiredEnv, strings.Join(missing, ", "))
	}

	// A networked deployment gets a slug unique across all deployments, so its
	// internal Service (octo-int-{slug}) — and thus its internal URL — never
	// collides with another deployment's, even another of the same integration.
	// The user picks the slug (a free default is suggested up front); an empty slug
	// asks the orchestrator to allocate one.
	slug := ""
	internalURL := ""
	if networked {
		slug, err = s.resolveSlug(ctx, settings.Slug, slugify(it.Name))
		if err != nil {
			return Deployment{}, err
		}
		internalURL = s.kube.InternalURL(slug, port)
	}

	// Optional external endpoint: validate up front so a bad request fails before
	// any row or resource is created.
	external := settings.External()
	subdomain := ""
	externalURL := ""
	if external {
		if !s.kube.ExternalEnabled() {
			return Deployment{}, ErrExternalUnavailable
		}
		if !networked {
			// The integration declares no HTTP_PORT, so it has no HTTP source to
			// reach from outside the cluster. Downgrade to a bare internal workload.
			slog.Info("deployment not externally exposable; falling back to internal-only",
				"integrationId", integrationID, "name", it.Name)
			external = false
		} else {
			want := settings.Subdomain
			if want == "" {
				want = slug // default the external host to the unique slug
			}
			subdomain = slugify(want)
			if subdomain == "" {
				return Deployment{}, ErrInvalidSubdomain
			}
			if err := s.ensureSubdomainUnique(ctx, integrationID, subdomain); err != nil {
				return Deployment{}, err
			}
			externalURL = s.kube.ExternalURL(subdomain)
		}
	}

	runner, err := s.resolveRunner(settings.Runner)
	if err != nil {
		return Deployment{}, err
	}

	persisted := Settings{Replicas: replicas}
	if external {
		persisted.Expose = ExposeExternal
		persisted.Subdomain = subdomain
	}
	// Persist the env bindings so reads, the SSE snapshot and the in-use check see
	// them. Literal values are stored as-is; secret bindings hold only the name.
	persisted.Env = settings.Env
	persisted.Tracing = settings.Tracing
	// The platform-access grants are persisted for the same reason as the env
	// bindings: they are what the next rollout starts from, and what a future access
	// model reads to decide whether a call was ever meant to be allowed.
	persisted.OrchestratorAPI = settings.OrchestratorAPI
	persisted.ObservabilityAPI = settings.ObservabilityAPI
	// The runner is persisted for a reason worth naming, because this literal is a
	// field-by-field copy rather than a re-marshal of the request: a rollout reads
	// the stored row, so a runner that is not written here reaches the cluster on
	// the first deploy and is silently demoted to the standard image on the first
	// rollout — which for an agentic deployment means every one of its commands
	// stops working after an upgrade nobody connected to it.
	persisted.Runner = settings.Runner
	persisted.SnapshotID = snapID
	settingsJSON, err := json.Marshal(persisted)
	if err != nil {
		return Deployment{}, err
	}
	metadata, err := json.Marshal(Metadata{
		Name:        it.Name,
		Slug:        slug,
		InternalURL: internalURL,
		ExternalURL: externalURL,
		SnapshotID:  snapID,
		Tag:         snapTag,
	})
	if err != nil {
		return Deployment{}, err
	}

	dep, err := s.repo.Create(ctx, integrationID, kube.StatusPending, settingsJSON, metadata)
	if err != nil {
		return Deployment{}, err
	}

	spec := kube.Spec{
		ID:               dep.ID,
		IntegrationID:    integrationID,
		Name:             it.Name,
		Version:          snapTag,
		SnapshotID:       snapID,
		Definition:       definition,
		Replicas:         int32(replicas),
		Slug:             slug,
		Port:             port,
		Env:              literalEnv,
		SecretEnv:        secretEnv,
		Expose:           external,
		Subdomain:        subdomain,
		Tracing:          settings.Tracing,
		ObservabilityAPI: settings.ObservabilityAPI,
		Runner:           runner,
	}
	if err := s.kube.Apply(ctx, spec); err != nil {
		// Roll back: remove any partially created resources and the row so the
		// failure is not left dangling. The per-deployment internal Service is named
		// from the slug (not the deployment id), so it is torn down separately.
		if delErr := s.kube.Delete(ctx, dep.ID); delErr != nil {
			slog.Error("deployment rollback: delete resources", "id", dep.ID, "error", delErr)
		}
		if slug != "" {
			if delErr := s.kube.DeleteInternalService(ctx, slug); delErr != nil {
				slog.Error("deployment rollback: delete internal service", "slug", slug, "error", delErr)
			}
		}
		if delErr := s.repo.Delete(ctx, dep.ID); delErr != nil {
			slog.Error("deployment rollback: delete row", "id", dep.ID, "error", delErr)
		}
		return Deployment{}, err
	}

	// Reflect the freshly observed status (typically still pending) in the cache.
	s.applyRefresh(ctx, &dep)
	return dep, nil
}

// resolveSnapshot loads the snapshot a deploy targets and verifies it belongs to
// integrationID. An empty id means the caller did not pick a version tag. The
// caller must only invoke this when a snapshot store is wired.
func (s *Service) resolveSnapshot(ctx context.Context, integrationID, snapshotID string) (snapshot.Snapshot, error) {
	if snapshotID == "" {
		return snapshot.Snapshot{}, ErrSnapshotRequired
	}
	snap, err := s.snapshots.Get(ctx, snapshotID)
	if err != nil {
		if errors.Is(err, snapshot.ErrNotFound) {
			return snapshot.Snapshot{}, ErrSnapshotNotFound
		}
		return snapshot.Snapshot{}, err
	}
	if snap.IntegrationID != integrationID {
		return snapshot.Snapshot{}, ErrSnapshotMismatch
	}
	return snap, nil
}

// resolveEnvBindings splits the user's per-deployment env bindings into the literal
// and secret-reference maps the kube spec needs. It starts the literal map from the
// orchestrator-supplied runtimeEnv (HTTP_PORT/HTTP_HOST), which is authoritative —
// a binding that targets those is rejected. A binding with a non-empty Secret is a
// reference (validated to exist in the cluster); otherwise it is a literal value.
func (s *Service) resolveEnvBindings(ctx context.Context, runtimeEnv map[string]string, bindings map[string]EnvBinding) (literal, secret map[string]string, err error) {
	literal = map[string]string{}
	for k, v := range runtimeEnv {
		literal[k] = v
	}
	for name, b := range bindings {
		if name == envHTTPPort || name == envHTTPHost || name == envLogsURL {
			return nil, nil, ErrReservedEnvVar
		}
		if b.Secret != "" {
			if secret == nil {
				secret = map[string]string{}
			}
			secret[name] = b.Secret
			continue
		}
		// Skip an empty binding: it must not set a blank container env var, which
		// would override a value the runtime loads from an .env file resource (container
		// env wins). An empty box means "no override" — let the file/default provide it.
		if b.Value == "" {
			continue
		}
		literal[name] = b.Value
	}
	for _, key := range secret {
		ok, exErr := s.kube.SecretKeyExists(ctx, key)
		if exErr != nil {
			return nil, nil, exErr
		}
		if !ok {
			return nil, nil, ErrSecretNotFound
		}
	}
	return literal, secret, nil
}

// DeployOptions reports the choices for a new deployment of an integration: whether
// it is networked (has an HTTP source, so it gets a slug and can be exposed) and a
// suggested free slug to prefill. When candidate is non-empty it instead validates
// that slug, returning its normalized form and whether it is well-formed and free.
type DeployOptions struct {
	Networked     bool
	SuggestedSlug string
	// EnvVars are the integration's declared environment variables (excluding the
	// orchestrator-managed HTTP_PORT/HTTP_HOST), for the modal to prompt on. Present
	// regardless of networked status.
	EnvVars []EnvVarDecl
	// EnvProvidedKeys are env var names already satisfied by the selected version's
	// frozen .env resources, sorted. The modal treats a required var in this set as
	// filled (the runtime reads it from the .env file), so it neither blocks the
	// deploy nor forces the operator to re-enter it.
	EnvProvidedKeys []string
	// Populated only when a candidate slug was supplied.
	SlugChecked   bool
	Slug          string // normalized (slugified) candidate
	SlugValid     bool   // candidate has a usable DNS-1123 form
	SlugAvailable bool   // candidate is not already claimed by a deployment
}

// DeployOptions resolves the deploy choices for integrationID. With an empty
// candidate it suggests a free slug; with a candidate it validates it for the given
// exposure (external also requires the subdomain to be free). When a snapshot
// (version tag) is selected its frozen definition drives networked-ness and the
// declared env vars, so the modal prompts match exactly what will deploy. It is
// read-only, so it does not require Kubernetes access.
func (s *Service) DeployOptions(ctx context.Context, integrationID, candidate string, external bool, snapshotID string) (DeployOptions, error) {
	it, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		if errors.Is(err, integration.ErrNotFound) {
			return DeployOptions{}, ErrIntegrationNotFound
		}
		return DeployOptions{}, err
	}
	// Prefer the selected snapshot's frozen definition; fall back to the live one
	// (no tag selected, or no snapshot store wired).
	definition := it.Definition
	if s.snapshots != nil && snapshotID != "" {
		snap, err := s.resolveSnapshot(ctx, integrationID, snapshotID)
		if err != nil {
			return DeployOptions{}, err
		}
		definition = snap.Definition
	}
	_, _, networked := resolveRuntimeEnv(definition)
	opts := DeployOptions{Networked: networked, EnvVars: declaredEnvVars(definition)}
	// Keys the .env resources already supply, so the modal doesn't block on (nor
	// force a value for) a required var a .env file covers — the operator may still
	// override it. A selected tag reads its frozen resources; Current reads the live
	// working copy (which a Current deploy tags first).
	var fileKeys map[string]struct{}
	if snapshotID != "" {
		fileKeys, err = s.snapshotEnvFileKeys(ctx, snapshotID)
	} else {
		fileKeys, err = s.liveEnvFileKeys(ctx, integrationID)
	}
	if err != nil {
		return DeployOptions{}, err
	}
	opts.EnvProvidedKeys = sortedKeys(fileKeys)
	if !networked {
		return opts, nil
	}
	if candidate != "" {
		opts.SlugChecked = true
		opts.Slug = slugify(candidate)
		opts.SlugValid = opts.Slug != ""
		if opts.SlugValid {
			free, err := s.addressFree(ctx, opts.Slug, external)
			if err != nil {
				return DeployOptions{}, err
			}
			opts.SlugAvailable = free
		}
		return opts, nil
	}
	opts.SuggestedSlug, err = s.allocateSlug(ctx, slugify(it.Name))
	if err != nil {
		return DeployOptions{}, err
	}
	return opts, nil
}

// addEnvKeys folds a resource's .env keys into dst when it is an env resource.
func addEnvKeys(dst map[string]struct{}, kind, content string) {
	if kind == resource.KindEnv {
		for k := range parseDotEnvKeys(content) {
			dst[k] = struct{}{}
		}
	}
}

// snapshotEnvFileKeys is the set of env var names supplied by a snapshot's frozen
// .env resources. Empty when no snapshot store is wired or no tag is selected.
// Consulted by Deploy (to allow the deploy) so it never disagrees with the modal.
func (s *Service) snapshotEnvFileKeys(ctx context.Context, snapshotID string) (map[string]struct{}, error) {
	keys := map[string]struct{}{}
	if s.snapshots == nil || snapshotID == "" {
		return keys, nil
	}
	resources, err := s.snapshots.ListResources(ctx, snapshotID)
	if err != nil {
		return nil, err
	}
	for _, res := range resources {
		addEnvKeys(keys, res.Kind, res.Content)
	}
	return keys, nil
}

// liveEnvFileKeys is the set of env var names supplied by an integration's live
// .env resources (the working copy). Empty when no resource store is wired. Used
// by DeployOptions for the Current case, where the deploy tags the working copy
// first — its .env keys are exactly the live ones.
func (s *Service) liveEnvFileKeys(ctx context.Context, integrationID string) (map[string]struct{}, error) {
	keys := map[string]struct{}{}
	if s.resources == nil {
		return keys, nil
	}
	resources, err := s.resources.ListByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	for _, res := range resources {
		addEnvKeys(keys, res.Kind, res.Content)
	}
	return keys, nil
}

// sortedKeys returns a set's keys as a sorted slice, for a stable wire response.
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// resolveSlug returns the slug to use for a networked deployment. A user-supplied
// slug is normalized and must be free as an internal slug (the subdomain is checked
// separately when the deployment is external); an empty slug is auto-allocated.
func (s *Service) resolveSlug(ctx context.Context, chosen, base string) (string, error) {
	if chosen == "" {
		return s.allocateSlug(ctx, base)
	}
	slug := slugify(chosen)
	if slug == "" {
		return "", ErrInvalidSlug
	}
	_, found, err := s.repo.IntegrationIDBySlug(ctx, slug)
	if err != nil {
		return "", err
	}
	if found {
		return "", ErrSlugTaken
	}
	return slug, nil
}

// addressFree reports whether slug is available as a deployment address: the
// internal slug must be unclaimed, and for an external deployment the subdomain
// (which defaults to the slug) must be unclaimed too.
func (s *Service) addressFree(ctx context.Context, slug string, external bool) (bool, error) {
	if _, found, err := s.repo.IntegrationIDBySlug(ctx, slug); err != nil {
		return false, err
	} else if found {
		return false, nil
	}
	if !external {
		return true, nil
	}
	_, found, err := s.repo.IntegrationIDBySubdomain(ctx, slug)
	if err != nil {
		return false, err
	}
	return !found, nil
}

// allocateSlug returns a DNS-1123 slug unique across all deployments, used to name
// the per-deployment internal Service (octo-int-{slug}) so each deployment has its
// own internal URL. It starts from base (the integration name slug) and appends a
// -NNN suffix, scanning until it finds a value no deployment already claims.
func (s *Service) allocateSlug(ctx context.Context, base string) (string, error) {
	const maxLen = 54 // 63 - len("octo-int-")
	if base == "" {
		base = "integration"
	}
	if len(base) > maxLen {
		base = strings.Trim(base[:maxLen], "-")
	}
	for i := 0; i <= 999; i++ {
		candidate := base
		if i > 0 {
			suffix := fmt.Sprintf("-%03d", i)
			trimmed := base
			if len(trimmed)+len(suffix) > maxLen {
				trimmed = strings.Trim(trimmed[:maxLen-len(suffix)], "-")
			}
			candidate = trimmed + suffix
		}
		_, found, err := s.repo.IntegrationIDBySlug(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !found {
			return candidate, nil
		}
	}
	return "", ErrSlugExhausted
}

// ensureSubdomainUnique rejects a deploy whose external subdomain is already
// claimed by a different integration, so external hosts never collide. An empty
// subdomain (internal-only) is skipped.
func (s *Service) ensureSubdomainUnique(ctx context.Context, integrationID, subdomain string) error {
	if subdomain == "" {
		return nil
	}
	owner, found, err := s.repo.IntegrationIDBySubdomain(ctx, subdomain)
	if err != nil {
		return err
	}
	if found && owner != integrationID {
		return ErrSubdomainTaken
	}
	return nil
}

// Scale changes the desired replica count of an existing deployment: it updates
// the cluster workload, then persists the new count in the settings so reads and
// the SSE snapshot reflect it. replicas <1 is normalized to 1. The returned
// Deployment carries its freshly refreshed live status.
func (s *Service) Scale(ctx context.Context, id string, replicas int) (Deployment, error) {
	if s.kube == nil {
		return Deployment{}, ErrUnavailable
	}
	if replicas < 1 {
		replicas = 1
	}
	dep, err := s.repo.Get(ctx, id)
	if err != nil {
		return Deployment{}, err
	}
	if err := s.kube.Scale(ctx, id, int32(replicas)); err != nil {
		return Deployment{}, err
	}

	// Persist the new replica count, preserving the rest of the settings.
	settings := ParseSettings(dep.Settings)
	settings.Replicas = replicas
	raw, err := json.Marshal(settings)
	if err != nil {
		return Deployment{}, err
	}
	if err := s.repo.UpdateSettings(ctx, id, raw); err != nil {
		return Deployment{}, err
	}
	dep.Settings = raw

	s.applyRefresh(ctx, &dep)
	return dep, nil
}

// Rollout upgrades a live deployment to a different version tag in place: it ships
// the new snapshot's frozen definition as a rolling update, preserving the
// deployment's id, address (slug/URLs), scale, env bindings and tracing setting,
// and records the new tag. It is also the path that edits env or tracing on a live
// deployment, since both only reach the runtime by replacing its pods.
// A tag that changes the integration's HTTP source (networked vs not) would
// change the Service/Ingress topology, which a rolling update cannot express, so it
// is rejected — undeploy and redeploy instead.
func (s *Service) Rollout(ctx context.Context, id, snapshotID string, env map[string]EnvBinding, tracing *bool) (Deployment, error) {
	if s.kube == nil {
		return Deployment{}, ErrUnavailable
	}
	if s.snapshots == nil {
		return Deployment{}, ErrSnapshotRequired
	}
	dep, err := s.repo.Get(ctx, id)
	if err != nil {
		return Deployment{}, err
	}
	snap, err := s.resolveSnapshot(ctx, dep.IntegrationID, snapshotID)
	if err != nil {
		return Deployment{}, err
	}

	settings := ParseSettings(dep.Settings)
	meta := ParseMetadata(dep.Metadata)

	// An explicit env set replaces the deployment's stored bindings (the operator
	// edited/extended env on rollout); a nil map preserves them (a plain version
	// bump keeps the same env).
	if env != nil {
		settings.Env = env
	}

	// Same convention for tracing: nil leaves the deployment's stored setting alone
	// (a plain version bump keeps whatever it was running with), a value sets it.
	// Rollout is how tracing is switched on or off for a deployment that is already
	// live — the runtime reads OCTO_TRACING from its process environment when it
	// parses flags, so the change only reaches it by replacing the pods.
	if tracing != nil {
		settings.Tracing = *tracing
	}

	// The runtime port/env come from the new frozen definition. A networked
	// deployment has a slug (and Service); flipping networked-ness would add or
	// remove that Service/Ingress, so reject it.
	port, runtimeEnv, networked := resolveRuntimeEnv(snap.Definition)
	if networked != (meta.Slug != "") {
		return Deployment{}, ErrRolloutTopologyChange
	}

	literalEnv, secretEnv, err := s.resolveEnvBindings(ctx, runtimeEnv, settings.Env)
	if err != nil {
		return Deployment{}, err
	}

	// Reject a rollover whose target version declares a required env var that nothing
	// provides — neither a binding nor the tag's frozen .env resources. Mirrors Deploy
	// so a version bump that adds a required var fails fast with a clear message
	// instead of leaving the pod to crash-loop on load.
	provided := providedEnvKeys(settings.Env)
	fileKeys, err := s.snapshotEnvFileKeys(ctx, snap.ID)
	if err != nil {
		return Deployment{}, err
	}
	for k := range fileKeys {
		provided[k] = struct{}{}
	}
	if missing := missingRequiredEnv(snap.Definition, provided); len(missing) > 0 {
		return Deployment{}, fmt.Errorf("%w: %s", ErrMissingRequiredEnv, strings.Join(missing, ", "))
	}

	replicas := settings.Replicas
	if replicas < 1 {
		replicas = 1
	}

	// The runner comes from the stored row, not from this call: a rollout replaces
	// the version, never the shape of the workload. Resolving it again here is what
	// catches an install that has since dropped the agentic image — better to refuse
	// the rollout than to replace a working agentic pod with a distroless one whose
	// every command fails.
	runner, err := s.resolveRunner(settings.Runner)
	if err != nil {
		return Deployment{}, err
	}

	// Same id/slug/exposure as the existing deployment, so its address and Services
	// are untouched; only the mounted definition (and any env/port it implies)
	// changes. NOTE: a tag that changes HTTP_PORT updates the container port but not
	// the existing Service's targetPort — tags of one integration normally share a
	// port, so this edge is left for a future enhancement.
	spec := kube.Spec{
		ID:               dep.ID,
		IntegrationID:    dep.IntegrationID,
		Name:             meta.Name,
		Version:          snap.Tag,
		SnapshotID:       snap.ID,
		Definition:       snap.Definition,
		Replicas:         int32(replicas),
		Slug:             meta.Slug,
		Port:             port,
		Env:              literalEnv,
		SecretEnv:        secretEnv,
		Expose:           settings.External(),
		Subdomain:        settings.Subdomain,
		Tracing:          settings.Tracing,
		ObservabilityAPI: settings.ObservabilityAPI,
		Runner:           runner,
	}
	if err := s.kube.Rollout(ctx, spec); err != nil {
		return Deployment{}, err
	}

	// Record the new tag in metadata and the new snapshot id in settings (as Deploy
	// does), so reads and the SSE snapshot reflect what is now running.
	//
	// Both in one write, and the error returned rather than logged. The stored
	// settings are not just a record of this rollout, they are the input to the
	// next one: env and tracing both follow "nil preserves, present replaces", so
	// a rollout that replaced either and then failed to persist leaves the pods on
	// the new value and the row on the old — and the next plain version bump reads
	// the stale row and quietly undoes the change. Reporting success there is the
	// part that makes it invisible.
	//
	// The caller sees a failure for a rollout whose pods did roll. That is the
	// honest report and the safe direction: the cluster is the harder thing to
	// undo, and a rollout is idempotent, so retrying costs nothing.
	meta.SnapshotID = snap.ID
	meta.Tag = snap.Tag
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return Deployment{}, fmt.Errorf("rollout %s: marshal metadata: %w", id, err)
	}
	settings.SnapshotID = snap.ID
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return Deployment{}, fmt.Errorf("rollout %s: marshal settings: %w", id, err)
	}
	if err := s.repo.UpdateMetadataAndSettings(ctx, id, metaJSON, settingsJSON); err != nil {
		return Deployment{}, fmt.Errorf(
			"rollout %s: pods rolled to %s but recording it failed, so the stored "+
				"deployment is behind them; retry the rollout: %w", id, snap.Tag, err)
	}
	dep.Metadata = metaJSON
	dep.Settings = settingsJSON

	s.applyRefresh(ctx, &dep)
	return dep, nil
}

// Get returns a deployment with its status refreshed from the cluster.
func (s *Service) Get(ctx context.Context, id string) (Deployment, error) {
	dep, err := s.repo.Get(ctx, id)
	if err != nil {
		return Deployment{}, err
	}
	s.applyRefresh(ctx, &dep)
	return dep, nil
}

// PodLogs streams the logs of one of a deployment's pods. It verifies the
// deployment exists (ErrNotFound) and that the pod currently belongs to it
// (ErrPodNotFound) before opening the stream, so a caller cannot read arbitrary
// pods in the namespace by id-guessing. With follow set the stream tails the pod
// live until the caller closes the returned reader or the context is cancelled.
func (s *Service) PodLogs(ctx context.Context, deploymentID, podName string, follow bool, tail int64) (io.ReadCloser, error) {
	if s.kube == nil {
		return nil, ErrUnavailable
	}
	if _, err := s.repo.Get(ctx, deploymentID); err != nil {
		return nil, err
	}
	st, err := s.kube.Status(ctx, deploymentID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, p := range st.Pods {
		if p.Name == podName {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrPodNotFound
	}
	// Named explicitly even though a deployment pod has exactly one container: the
	// name is now part of the call, and passing "" here would only work by accident
	// of that fact.
	return s.kube.PodLogs(ctx, podName, kube.RuntimeContainer, follow, tail)
}

// ListByIntegration returns an integration's deployments, each with its status
// refreshed from the cluster.
func (s *Service) ListByIntegration(ctx context.Context, integrationID string) ([]Deployment, error) {
	deps, err := s.repo.ListByIntegration(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	for i := range deps {
		s.applyRefresh(ctx, &deps[i])
	}
	return deps, nil
}

// Undeploy deletes the cluster resources and then the row. It verifies the row
// exists first so callers get ErrNotFound for an unknown id. The internal Service
// is per-deployment (named from the deployment's unique slug), so it is removed
// together with its deployment.
func (s *Service) Undeploy(ctx context.Context, id string) error {
	if s.kube == nil {
		return ErrUnavailable
	}
	dep, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.kube.Delete(ctx, id); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}

	// Drop this deployment's internal Service (present only for networked ones).
	if slug := ParseMetadata(dep.Metadata).Slug; slug != "" {
		if err := s.kube.DeleteInternalService(ctx, slug); err != nil {
			slog.Error("undeploy: delete internal service", "slug", slug, "error", err)
		}
	}

	// Best-effort: drop this deployment's KV and secret entries. A failure here must
	// not fail the undeploy — the rows are harmless orphans, scoped to a now-gone
	// deployment.
	for _, c := range s.cleaners {
		if err := c.DeleteByDeployment(ctx, id); err != nil {
			slog.Error("undeploy: store cleanup", "deployment", id, "error", err)
		}
	}
	return nil
}

// slugify reduces an integration name to a DNS-1123 label usable as the internal
// Service suffix: lowercase, runs of non-alphanumerics collapsed to single
// dashes, trimmed, and bounded so "octo-int-"+slug stays within 63 chars.
// Returns "" when nothing usable remains (the caller then skips the Service).
func slugify(name string) string {
	const maxLen = 54 // 63 - len("octo-int-")
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case b.Len() > 0 && !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > maxLen {
		out = strings.Trim(out[:maxLen], "-")
	}
	return out
}

// applyRefresh refreshes d's live status from the cluster in place: it sets the
// coarse cached Status (persisting it when it changed) and the live Detail.
func (s *Service) applyRefresh(ctx context.Context, d *Deployment) {
	st := s.refresh(ctx, d.ID, d.Status)
	d.Status = st.Phase
	d.Detail = st
}

// refresh queries the live status and updates the cache, returning the live
// status. On a Kubernetes/DB error it logs and falls back to the cached coarse
// value so a transient blip does not break a read.
func (s *Service) refresh(ctx context.Context, id, cached string) kube.Status {
	if s.kube == nil {
		return kube.Status{Phase: cached}
	}
	status, err := s.kube.Status(ctx, id)
	if err != nil {
		slog.Error("deployment status refresh", "id", id, "error", err)
		return kube.Status{Phase: cached}
	}
	if status.Phase != cached {
		if err := s.repo.UpdateStatus(ctx, id, status.Phase); err != nil {
			slog.Error("deployment status cache update", "id", id, "error", err)
		}
	}
	return status
}
