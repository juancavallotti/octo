package snapshot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

// maxTagLen bounds a tag; the column is unconstrained varchar so the limit is
// enforced here rather than by the database.
const maxTagLen = 64

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake; *Repo
// satisfies it structurally.
type repository interface {
	Create(ctx context.Context, integrationID, tag, definition string) (Snapshot, error)
	Get(ctx context.Context, id string) (Snapshot, error)
	ListByIntegration(ctx context.Context, integrationID string) ([]Snapshot, error)
	Delete(ctx context.Context, id string) error
}

// integrationStore is the slice of the integration repository the service needs
// to read the definition to freeze. *integration.Service satisfies it.
type integrationStore interface {
	Get(ctx context.Context, id string) (integration.Integration, error)
}

// Service holds snapshot business logic: tag validation and freezing the live
// integration definition under a tag.
type Service struct {
	repo         repository
	integrations integrationStore
}

// NewService returns a Service backed by repo and the integration store.
func NewService(repo repository, integrations integrationStore) *Service {
	return &Service{repo: repo, integrations: integrations}
}

// Create freezes integrationID's current definition under tag. The tag is
// validated and must be unique for the integration (ErrTagExists otherwise); an
// unknown integration yields ErrIntegrationNotFound.
func (s *Service) Create(ctx context.Context, integrationID, tag string) (Snapshot, error) {
	tag = strings.TrimSpace(tag)
	if err := validateTag(tag); err != nil {
		return Snapshot{}, err
	}
	it, err := s.integrations.Get(ctx, integrationID)
	if err != nil {
		if errors.Is(err, integration.ErrNotFound) {
			return Snapshot{}, ErrIntegrationNotFound
		}
		return Snapshot{}, err
	}
	return s.repo.Create(ctx, integrationID, tag, it.Definition)
}

// Get returns a snapshot by id.
func (s *Service) Get(ctx context.Context, id string) (Snapshot, error) {
	return s.repo.Get(ctx, id)
}

// ListByIntegration returns an integration's snapshots, newest first.
func (s *Service) ListByIntegration(ctx context.Context, integrationID string) ([]Snapshot, error) {
	return s.repo.ListByIntegration(ctx, integrationID)
}

// Delete removes a snapshot. Existing deployments keep the tag they recorded, so
// deleting a snapshot does not affect what is already running.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// validateTag enforces a non-empty, length-bounded tag drawn from a safe set of
// characters (letters, digits, dot, dash, underscore) so tags read cleanly in the
// UI and in URLs.
func validateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("%w: tag is required", ErrInvalid)
	}
	if len(tag) > maxTagLen {
		return fmt.Errorf("%w: tag must be at most %d characters", ErrInvalid, maxTagLen)
	}
	for _, r := range tag {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_'
		if !ok {
			return fmt.Errorf("%w: tag may contain only letters, digits, '.', '-' and '_'", ErrInvalid)
		}
	}
	return nil
}
