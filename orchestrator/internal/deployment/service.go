package deployment

import (
	"context"
	"encoding/json"
	"errors"
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
	slug := slugify(it.Name)

	// The runtime port (and the env the orchestrator supplies to bind it) come from
	// the integration's HTTP_PORT declaration; an integration is externally
	// exposable only when it declares one.
	port, runtimeEnv, exposable := resolveRuntimeEnv(it.Definition)

	// Optional external endpoint: validate up front so a bad request fails before
	// any row or resource is created.
	external := settings.External()
	subdomain := ""
	externalURL := ""
	if external {
		if !s.kube.ExternalEnabled() {
			return Deployment{}, ErrExternalUnavailable
		}
		if !exposable {
			// The integration declares no HTTP_PORT, so it cannot be reached from
			// outside the cluster. Downgrade to internal-only rather than failing.
			slog.Info("deployment not externally exposable; falling back to internal-only",
				"integrationId", integrationID, "name", it.Name)
			external = false
		} else {
			want := settings.Subdomain
			if want == "" {
				want = it.Name
			}
			subdomain = slugify(want)
			if subdomain == "" {
				return Deployment{}, ErrInvalidSubdomain
			}
			externalURL = s.kube.ExternalURL(subdomain)
		}
	}

	// Reject a slug/subdomain already owned by a different integration before
	// creating anything, so we never produce a colliding internal Service or host.
	if err := s.ensureUnique(ctx, integrationID, slug, subdomain); err != nil {
		return Deployment{}, err
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
		InternalURL: s.kube.InternalURL(slug, port),
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
		// failure is not left dangling.
		if delErr := s.kube.Delete(ctx, dep.ID); delErr != nil {
			slog.Error("deployment rollback: delete resources", "id", dep.ID, "error", delErr)
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

// ensureUnique verifies the slug and subdomain are not already claimed by a
// different integration's deployment. An empty value is skipped. A match owned by
// the same integration (a redeploy) is allowed: its deployments share the slug.
func (s *Service) ensureUnique(ctx context.Context, integrationID, slug, subdomain string) error {
	if slug != "" {
		owner, found, err := s.repo.IntegrationIDBySlug(ctx, slug)
		if err != nil {
			return err
		}
		if found && owner != integrationID {
			return ErrSlugTaken
		}
	}
	if subdomain != "" {
		owner, found, err := s.repo.IntegrationIDBySubdomain(ctx, subdomain)
		if err != nil {
			return err
		}
		if found && owner != integrationID {
			return ErrSubdomainTaken
		}
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
// exists first so callers get ErrNotFound for an unknown id. The stable internal
// Service is shared across an integration's deployments, so it is removed only
// once the last deployment of that integration is gone.
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

	// Drop the integration-scoped internal Service when no deployments remain.
	if slug := ParseMetadata(dep.Metadata).Slug; slug != "" {
		remaining, err := s.repo.ListByIntegration(ctx, dep.IntegrationID)
		if err != nil {
			slog.Error("undeploy: list remaining deployments", "integrationId", dep.IntegrationID, "error", err)
			return nil // the row is already gone; the orphaned Service is harmless
		}
		if len(remaining) == 0 {
			if err := s.kube.DeleteInternalService(ctx, slug); err != nil {
				slog.Error("undeploy: delete internal service", "slug", slug, "error", err)
			}
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
