package resource

import (
	"context"
	"fmt"
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

// Service holds resource business logic and validation.
type Service struct {
	repo repository
}

// NewService returns a Service backed by repo.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// Create validates the resource and persists it under integrationID.
func (s *Service) Create(ctx context.Context, integrationID, kind, name, content string) (Resource, error) {
	kind, name, err := validate(kind, name)
	if err != nil {
		return Resource{}, err
	}
	return s.repo.Create(ctx, integrationID, kind, name, content)
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
	return s.repo.Update(ctx, integrationID, id, kind, name, content)
}

// Delete removes a resource within integrationID.
func (s *Service) Delete(ctx context.Context, integrationID, id string) error {
	return s.repo.Delete(ctx, integrationID, id)
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
