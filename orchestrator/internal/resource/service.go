package resource

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// maxNameLen bounds a resource name; the column is unconstrained varchar so the
// limit is enforced here rather than by the database.
const maxNameLen = 512

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake without a
// database; *Repo satisfies it structurally. Get/Update/Delete take the owning
// integration id so a resource is only ever addressed within its integration —
// a mismatch reads as ErrNotFound.
type repository interface {
	Create(ctx context.Context, integrationID, kind, name, content string) (Resource, error)
	Get(ctx context.Context, integrationID, id string) (Resource, error)
	ListByIntegration(ctx context.Context, integrationID string) ([]Resource, error)
	Update(ctx context.Context, integrationID, id, kind, name, content string) (Resource, error)
	Delete(ctx context.Context, integrationID, id string) error
}

// reloadNotifier is told that an integration's stored state changed, so anything
// running it can pick the change up. Declared here, in the consumer, and satisfied
// structurally by *devrun.Service. A nil notifier means nothing is listening.
//
// Resources get the same treatment as the definition because a dev run loads them
// too: editing an env file is a change to what the running app does, and the local
// runner has always re-staged resources on sync.
type reloadNotifier interface {
	NotifyIntegrationChanged(ctx context.Context, integrationID string) error
}

// Service holds resource business logic and validation.
type Service struct {
	repo     repository
	notifier reloadNotifier
}

// Option customizes a Service at construction.
type Option func(*Service)

// WithReloadNotifier wires a notifier that is told, after a successful write, which
// integration's resources changed.
func WithReloadNotifier(n reloadNotifier) Option {
	return func(s *Service) { s.notifier = n }
}

// NewService returns a Service backed by repo.
func NewService(repo repository, opts ...Option) *Service {
	s := &Service{repo: repo}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// notifyChanged tells the notifier an integration's resources changed, after the
// write has landed. Best-effort and incapable of failing the caller; see
// integration.Service.notifyChanged for why.
func (s *Service) notifyChanged(ctx context.Context, integrationID string) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.NotifyIntegrationChanged(ctx, integrationID); err != nil {
		slog.Warn("resource changed but reload notification failed",
			"integrationId", integrationID, "error", err)
	}
}

// Create validates the resource and persists it under integrationID.
func (s *Service) Create(ctx context.Context, integrationID, kind, name, content string) (Resource, error) {
	kind, name, err := validate(kind, name)
	if err != nil {
		return Resource{}, err
	}
	created, err := s.repo.Create(ctx, integrationID, kind, name, content)
	if err != nil {
		return Resource{}, err
	}
	s.notifyChanged(ctx, integrationID)
	return created, nil
}

// Get returns the resource by id within integrationID.
func (s *Service) Get(ctx context.Context, integrationID, id string) (Resource, error) {
	return s.repo.Get(ctx, integrationID, id)
}

// ListByIntegration returns an integration's resources.
func (s *Service) ListByIntegration(ctx context.Context, integrationID string) ([]Resource, error) {
	return s.repo.ListByIntegration(ctx, integrationID)
}

// Update validates and persists changes to an existing resource within integrationID.
func (s *Service) Update(ctx context.Context, integrationID, id, kind, name, content string) (Resource, error) {
	kind, name, err := validate(kind, name)
	if err != nil {
		return Resource{}, err
	}
	updated, err := s.repo.Update(ctx, integrationID, id, kind, name, content)
	if err != nil {
		return Resource{}, err
	}
	s.notifyChanged(ctx, integrationID)
	return updated, nil
}

// Delete removes a resource within integrationID.
func (s *Service) Delete(ctx context.Context, integrationID, id string) error {
	if err := s.repo.Delete(ctx, integrationID, id); err != nil {
		return err
	}
	// A deletion is a change like any other, and the one that matters most to a
	// running dev run: the sidecar prunes files no longer declared, and it only knows
	// to look because of this.
	s.notifyChanged(ctx, integrationID)
	return nil
}

// validate normalizes and checks a resource's kind and name, returning the
// cleaned values. The name is trimmed of surrounding space; the kind is not.
func validate(kind, name string) (string, string, error) {
	if kind != KindEnv && kind != KindTemplate {
		return "", "", fmt.Errorf("%w: kind must be %q or %q", ErrInvalid, KindEnv, KindTemplate)
	}
	name = strings.TrimSpace(name)
	if err := validateName(name); err != nil {
		return "", "", err
	}
	return kind, name, nil
}

// validateName enforces a non-empty, length-bounded, path-like name. Names may
// contain '/' so a resource can carry a relative path (a future feature uploads
// integration zip bundles that keep their paths), but a name must not escape or
// be absolute: no "..", no leading '/', no empty segments. This mirrors the
// standalone filesystem loader's confinement without forbidding nesting.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("%w: name must be at most %d characters", ErrInvalid, maxNameLen)
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("%w: name must be relative (no leading '/')", ErrInvalid)
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" {
			return fmt.Errorf("%w: name must not contain empty path segments", ErrInvalid)
		}
		if seg == ".." {
			return fmt.Errorf("%w: name must not contain '..' path segments", ErrInvalid)
		}
	}
	return nil
}
