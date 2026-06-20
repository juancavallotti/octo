package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/juancavallotti/eip-go/orchestrator/internal/integration"
	"github.com/juancavallotti/eip-go/orchestrator/internal/kube"
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
	Delete(ctx context.Context, id string) error
}

// integrationStore is the slice of the integration repository the service needs
// to fetch the definition to deploy. *integration.Repo satisfies it.
type integrationStore interface {
	Get(ctx context.Context, id string) (integration.Integration, error)
}

// kubeClient is the Kubernetes surface the service drives. *kube.Client
// satisfies it.
type kubeClient interface {
	Apply(ctx context.Context, spec kube.Spec) error
	Status(ctx context.Context, deploymentID string) (kube.Status, error)
	Scale(ctx context.Context, deploymentID string, replicas int32) error
	Delete(ctx context.Context, deploymentID string) error
	InternalURL(slug string, port int) string
	DeleteInternalService(ctx context.Context, slug string) error
	ExternalEnabled() bool
	ExternalURL(subdomain string) string
}

// Service holds deployment lifecycle logic: it persists deployment rows and
// reconciles them with cluster resources via the kube client.
type Service struct {
	repo         repository
	integrations integrationStore
	kube         kubeClient
}

// NewService returns a Service. kube may be nil, in which case all operations
// return ErrUnavailable (the caller should not register the routes then).
func NewService(repo repository, integrations integrationStore, kube kubeClient) *Service {
	return &Service{repo: repo, integrations: integrations, kube: kube}
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

	replicas := settings.Replicas
	if replicas < 1 {
		replicas = 1
	}

	// The runtime port (and the env the orchestrator supplies to bind it) come from
	// the integration's HTTP_PORT declaration. Only an integration that declares one
	// has an HTTP source listening on a port — those are "networked" and get a
	// Service, a unique internal slug/URL and the option of external exposure.
	// Anything else (a timer, a scheduled job) runs as a bare workload, no Service.
	port, runtimeEnv, networked := resolveRuntimeEnv(it.Definition)

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

	persisted := Settings{Replicas: replicas}
	if external {
		persisted.Expose = ExposeExternal
		persisted.Subdomain = subdomain
	}
	settingsJSON, err := json.Marshal(persisted)
	if err != nil {
		return Deployment{}, err
	}
	metadata, err := json.Marshal(Metadata{
		Name:        it.Name,
		Slug:        slug,
		InternalURL: internalURL,
		ExternalURL: externalURL,
	})
	if err != nil {
		return Deployment{}, err
	}

	dep, err := s.repo.Create(ctx, integrationID, kube.StatusPending, settingsJSON, metadata)
	if err != nil {
		return Deployment{}, err
	}

	spec := kube.Spec{
		ID:            dep.ID,
		IntegrationID: integrationID,
		Definition:    it.Definition,
		Replicas:      int32(replicas),
		Slug:          slug,
		Port:          port,
		Env:           runtimeEnv,
		Expose:        external,
		Subdomain:     subdomain,
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

// DeployOptions reports the choices for a new deployment of an integration: whether
// it is networked (has an HTTP source, so it gets a slug and can be exposed) and a
// suggested free slug to prefill. When candidate is non-empty it instead validates
// that slug, returning its normalized form and whether it is well-formed and free.
type DeployOptions struct {
	Networked     bool
	SuggestedSlug string
	// Populated only when a candidate slug was supplied.
	SlugChecked   bool
	Slug          string // normalized (slugified) candidate
	SlugValid     bool   // candidate has a usable DNS-1123 form
	SlugAvailable bool   // candidate is not already claimed by a deployment
}

// DeployOptions resolves the deploy choices for integrationID. With an empty
// candidate it suggests a free slug; with a candidate it validates it for the given
// exposure (external also requires the subdomain to be free). It is read-only, so
// it does not require Kubernetes access.
func (s *Service) DeployOptions(ctx context.Context, integrationID, candidate string, external bool) (DeployOptions, error) {
	it, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		if errors.Is(err, integration.ErrNotFound) {
			return DeployOptions{}, ErrIntegrationNotFound
		}
		return DeployOptions{}, err
	}
	_, _, networked := resolveRuntimeEnv(it.Definition)
	opts := DeployOptions{Networked: networked}
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

// Get returns a deployment with its status refreshed from the cluster.
func (s *Service) Get(ctx context.Context, id string) (Deployment, error) {
	dep, err := s.repo.Get(ctx, id)
	if err != nil {
		return Deployment{}, err
	}
	s.applyRefresh(ctx, &dep)
	return dep, nil
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
