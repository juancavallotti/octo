package integration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// maxNameLen bounds an integration name; the column is unconstrained varchar so
// the limit is enforced here rather than by the database.
const maxNameLen = 200

// repository is the persistence surface the service needs. It is declared in
// the consumer (and unexported) so service tests can substitute a fake without
// a database; *Repo satisfies it structurally.
type repository interface {
	Create(ctx context.Context, name, definition, actorID string) (Integration, error)
	Get(ctx context.Context, id string) (Integration, error)
	List(ctx context.Context) ([]Integration, error)
	Update(ctx context.Context, id, name, definition, actorID string) (Integration, error)
	Delete(ctx context.Context, id string) error
	NameExists(ctx context.Context, name, excludeID string) (bool, error)
}

// reloadNotifier is told that an integration's stored state changed, so anything
// running it can pick the change up. Declared here, in the consumer, and satisfied
// structurally by *devrun.Service — this package never learns that dev runs exist.
// A nil notifier means nothing is listening.
type reloadNotifier interface {
	NotifyIntegrationChanged(ctx context.Context, integrationID string) error
}

// Service holds integration business logic and validation.
type Service struct {
	repo     repository
	notifier reloadNotifier
}

// Option customizes a Service at construction.
type Option func(*Service)

// WithReloadNotifier wires a notifier that is told, after a successful write, which
// integration changed. This is the reload trigger for dev runs, and it lives here —
// at the write — rather than in the editor, so that every writer is covered at once:
// the editor's save, the integrations list, MCP's update_flow, any API-key client.
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

// notifyChanged tells the notifier an integration changed, after the write has
// landed.
//
// Best-effort, and deliberately incapable of failing the caller: a save that
// succeeded with a reload that did not is a stale running app, which the run's own
// status surface reports; a save reported as failed because a pod was unreachable is
// data loss. It is also called only after the write commits, so a puller cannot race
// ahead of the data it is pulling.
func (s *Service) notifyChanged(ctx context.Context, id string) {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.NotifyIntegrationChanged(ctx, id); err != nil {
		slog.Warn("integration changed but reload notification failed", "integrationId", id, "error", err)
	}
}

// Create validates the name and persists a new integration. actorID is the
// creating user's id, or "" when unknown (e.g. the MCP path or local dev without
// SSO); it is recorded as both created_by and updated_by.
func (s *Service) Create(ctx context.Context, name, definition, actorID string) (Integration, error) {
	if err := validateName(name); err != nil {
		return Integration{}, err
	}
	trimmed := strings.TrimSpace(name)
	if err := s.ensureNameFree(ctx, trimmed, ""); err != nil {
		return Integration{}, err
	}
	return s.repo.Create(ctx, trimmed, definition, actorID)
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
// actorID is the editing user's id, or "" when unknown; it is recorded as
// updated_by (created_by is left untouched).
func (s *Service) Update(ctx context.Context, id, name, definition, actorID string) (Integration, error) {
	if err := validateName(name); err != nil {
		return Integration{}, err
	}
	trimmed := strings.TrimSpace(name)
	if err := s.ensureNameFree(ctx, trimmed, id); err != nil {
		return Integration{}, err
	}
	updated, err := s.repo.Update(ctx, id, trimmed, definition, actorID)
	if err != nil {
		return Integration{}, err
	}
	s.notifyChanged(ctx, id)
	return updated, nil
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
