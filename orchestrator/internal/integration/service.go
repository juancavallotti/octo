package integration

import (
	"context"
	"fmt"
	"strings"
)

// maxNameLen bounds an integration name; the column is unconstrained varchar so
// the limit is enforced here rather than by the database.
const maxNameLen = 200

// repository is the persistence surface the service needs. It is declared in
// the consumer (and unexported) so service tests can substitute a fake without
// a database; *Repo satisfies it structurally.
type repository interface {
	Create(ctx context.Context, name, definition string) (Integration, error)
	Get(ctx context.Context, id string) (Integration, error)
	List(ctx context.Context) ([]Integration, error)
	Update(ctx context.Context, id, name, definition string) (Integration, error)
	Delete(ctx context.Context, id string) error
	NameExists(ctx context.Context, name, excludeID string) (bool, error)
}

// Service holds integration business logic and validation.
type Service struct {
	repo repository
}

// NewService returns a Service backed by repo.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// Create validates the name and persists a new integration.
func (s *Service) Create(ctx context.Context, name, definition string) (Integration, error) {
	if err := validateName(name); err != nil {
		return Integration{}, err
	}
	trimmed := strings.TrimSpace(name)
	if err := s.ensureNameFree(ctx, trimmed, ""); err != nil {
		return Integration{}, err
	}
	return s.repo.Create(ctx, trimmed, definition)
}

// Get returns the integration by id.
func (s *Service) Get(ctx context.Context, id string) (Integration, error) {
	return s.repo.Get(ctx, id)
}

// List returns all integrations.
func (s *Service) List(ctx context.Context) ([]Integration, error) {
	return s.repo.List(ctx)
}

// Update validates the name and persists changes to an existing integration.
func (s *Service) Update(ctx context.Context, id, name, definition string) (Integration, error) {
	if err := validateName(name); err != nil {
		return Integration{}, err
	}
	trimmed := strings.TrimSpace(name)
	if err := s.ensureNameFree(ctx, trimmed, id); err != nil {
		return Integration{}, err
	}
	return s.repo.Update(ctx, id, trimmed, definition)
}

// ensureNameFree rejects a name already used by a different integration
// (case-insensitive). excludeID is the integration being renamed, or "" on
// create. The unique index on lower(name) is the ultimate backstop; this gives
// a clean domain error instead of a raw constraint violation.
func (s *Service) ensureNameFree(ctx context.Context, name, excludeID string) error {
	taken, err := s.repo.NameExists(ctx, name, excludeID)
	if err != nil {
		return err
	}
	if taken {
		return fmt.Errorf("%w: %q", ErrNameTaken, name)
	}
	return nil
}

// Delete removes an integration.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// validateName enforces a non-empty, length-bounded name.
func validateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if len(trimmed) > maxNameLen {
		return fmt.Errorf("%w: name must be at most %d characters", ErrInvalid, maxNameLen)
	}
	return nil
}
