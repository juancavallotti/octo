package folder

import (
	"context"
	"fmt"
	"strings"

	"github.com/juancavallotti/eip-go/orchestrator/internal/integration"
)

// maxNameLen bounds a folder name; the column is unconstrained varchar so the
// limit is enforced here rather than by the database.
const maxNameLen = 200

// repository is the persistence surface the service needs. It is declared in the
// consumer (and unexported) so service tests can substitute a fake without a
// database; *Repo satisfies it structurally.
type repository interface {
	Create(ctx context.Context, name string, parentID *string) (Folder, error)
	Get(ctx context.Context, id string) (Folder, error)
	List(ctx context.Context) ([]Folder, error)
	Update(ctx context.Context, id, name string, parentID *string) (Folder, error)
	Delete(ctx context.Context, id string) error
	AddIntegration(ctx context.Context, folderID, integrationID string) error
	RemoveIntegration(ctx context.Context, folderID, integrationID string) error
	ListIntegrations(ctx context.Context, folderID string) ([]integration.Integration, error)
}

// Service holds folder business logic: name validation, cycle-safe moves and
// tree assembly.
type Service struct {
	repo repository
}

// NewService returns a Service backed by repo.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// Create validates the name and persists a new folder under parentID (nil for a
// root).
func (s *Service) Create(ctx context.Context, name string, parentID *string) (Folder, error) {
	if err := validateName(name); err != nil {
		return Folder{}, err
	}
	return s.repo.Create(ctx, strings.TrimSpace(name), parentID)
}

// Get returns the folder by id.
func (s *Service) Get(ctx context.Context, id string) (Folder, error) {
	return s.repo.Get(ctx, id)
}

// Tree returns the folders assembled into a nested forest: root folders, each
// carrying its descendants in Children. Sibling order follows the repository's
// name ordering.
func (s *Service) Tree(ctx context.Context) ([]Folder, error) {
	flat, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	return buildTree(flat), nil
}

// Update validates the name, rejects moves that would create a cycle, and
// persists the rename/reparent (parentID nil moves the folder to a root).
func (s *Service) Update(ctx context.Context, id, name string, parentID *string) (Folder, error) {
	if err := validateName(name); err != nil {
		return Folder{}, err
	}
	if parentID != nil {
		if err := s.checkMove(ctx, id, *parentID); err != nil {
			return Folder{}, err
		}
	}
	return s.repo.Update(ctx, id, strings.TrimSpace(name), parentID)
}

// Delete removes a folder (the schema cascades to children and membership).
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// AddIntegration places an integration in a folder, moving it if it already
// belonged elsewhere.
func (s *Service) AddIntegration(ctx context.Context, folderID, integrationID string) error {
	return s.repo.AddIntegration(ctx, folderID, integrationID)
}

// RemoveIntegration removes an integration from a folder.
func (s *Service) RemoveIntegration(ctx context.Context, folderID, integrationID string) error {
	return s.repo.RemoveIntegration(ctx, folderID, integrationID)
}

// ListIntegrations returns the integrations that belong to a folder.
func (s *Service) ListIntegrations(ctx context.Context, folderID string) ([]integration.Integration, error) {
	return s.repo.ListIntegrations(ctx, folderID)
}

// checkMove rejects reparenting id under newParentID when that would create a
// cycle: either id is its own new parent, or newParentID lies within id's
// subtree. A missing newParentID is left to the repository's foreign-key check.
func (s *Service) checkMove(ctx context.Context, id, newParentID string) error {
	if newParentID == id {
		return fmt.Errorf("%w: a folder cannot be its own parent", ErrInvalid)
	}
	folders, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	parentOf := make(map[string]*string, len(folders))
	for _, f := range folders {
		parentOf[f.ID] = f.ParentID
	}
	// Walk ancestors of the proposed parent; if we reach id, id is an ancestor of
	// newParentID, so moving id under it would close a loop. The step cap guards
	// against a pre-existing cycle in the data.
	cur := newParentID
	for range parentOf {
		if cur == id {
			return fmt.Errorf("%w: move would create a cycle", ErrInvalid)
		}
		p, ok := parentOf[cur]
		if !ok || p == nil {
			return nil
		}
		cur = *p
	}
	return nil
}

// buildTree assembles a flat, name-ordered folder list into a nested forest.
func buildTree(flat []Folder) []Folder {
	childrenOf := make(map[string][]Folder)
	roots := make([]Folder, 0)
	for _, f := range flat {
		if f.ParentID == nil {
			roots = append(roots, f)
			continue
		}
		childrenOf[*f.ParentID] = append(childrenOf[*f.ParentID], f)
	}

	var attach func(f Folder) Folder
	attach = func(f Folder) Folder {
		kids := childrenOf[f.ID]
		f.Children = make([]Folder, 0, len(kids))
		for _, k := range kids {
			f.Children = append(f.Children, attach(k))
		}
		return f
	}

	out := make([]Folder, 0, len(roots))
	for _, r := range roots {
		out = append(out, attach(r))
	}
	return out
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
