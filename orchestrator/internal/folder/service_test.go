package folder

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

// fakeRepo is a hand-written repository for service unit tests: it captures the
// arguments it receives and returns canned results, so validation, cycle checks
// and tree assembly can be exercised without a database.
type fakeRepo struct {
	listFolders []Folder
	listErr     error

	createName   string
	createParent *string

	updateID     string
	updateName   string
	updateParent *string

	getErr           error
	deleteErr        error
	addErr           error
	removeErr        error
	listIntErr       error
	listIntegrations []integration.Integration

	reorderFolderID string
	reorderIDs      []string
	reorderErr      error

	reorderParentID   *string
	reorderFolderIDs  []string
	reorderFoldersErr error
}

func (f *fakeRepo) Create(_ context.Context, name string, parentID *string) (Folder, error) {
	f.createName = name
	f.createParent = parentID
	return Folder{ID: "new", Name: name, ParentID: parentID}, nil
}

func (f *fakeRepo) Get(_ context.Context, id string) (Folder, error) {
	if f.getErr != nil {
		return Folder{}, f.getErr
	}
	return Folder{ID: id}, nil
}

func (f *fakeRepo) List(_ context.Context) ([]Folder, error) {
	return f.listFolders, f.listErr
}

func (f *fakeRepo) Update(_ context.Context, id, name string, parentID *string) (Folder, error) {
	f.updateID = id
	f.updateName = name
	f.updateParent = parentID
	return Folder{ID: id, Name: name, ParentID: parentID}, nil
}

func (f *fakeRepo) Delete(_ context.Context, _ string) error            { return f.deleteErr }
func (f *fakeRepo) AddIntegration(_ context.Context, _, _ string) error { return f.addErr }
func (f *fakeRepo) RemoveIntegration(_ context.Context, _, _ string) error {
	return f.removeErr
}

func (f *fakeRepo) ListIntegrations(_ context.Context, _ string) ([]integration.Integration, error) {
	return f.listIntegrations, f.listIntErr
}

func (f *fakeRepo) ReorderIntegrations(_ context.Context, folderID string, ids []string) error {
	f.reorderFolderID = folderID
	f.reorderIDs = ids
	return f.reorderErr
}

func (f *fakeRepo) ReorderFolders(_ context.Context, parentID *string, ids []string) error {
	f.reorderParentID = parentID
	f.reorderFolderIDs = ids
	return f.reorderFoldersErr
}

func TestReorderIntegrations(t *testing.T) {
	t.Run("passes the order through to the repository", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo)
		ids := []string{"a", "b", "c"}
		if err := svc.ReorderIntegrations(context.Background(), "folder-1", ids); err != nil {
			t.Fatalf("ReorderIntegrations: %v", err)
		}
		if repo.reorderFolderID != "folder-1" {
			t.Errorf("folder id = %q, want folder-1", repo.reorderFolderID)
		}
		if len(repo.reorderIDs) != 3 || repo.reorderIDs[0] != "a" || repo.reorderIDs[2] != "c" {
			t.Errorf("ids = %v, want [a b c]", repo.reorderIDs)
		}
	})

	t.Run("an empty list is a no-op", func(t *testing.T) {
		repo := &fakeRepo{reorderErr: errors.New("should not be called")}
		svc := NewService(repo)
		if err := svc.ReorderIntegrations(context.Background(), "folder-1", nil); err != nil {
			t.Fatalf("empty reorder should be a no-op, got %v", err)
		}
		if repo.reorderIDs != nil {
			t.Errorf("repo should not have been called, got ids %v", repo.reorderIDs)
		}
	})
}

func TestReorderFolders(t *testing.T) {
	t.Run("passes the parent and order through to the repository", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo)
		if err := svc.ReorderFolders(context.Background(), ptr("p1"), []string{"a", "b"}); err != nil {
			t.Fatalf("ReorderFolders: %v", err)
		}
		if repo.reorderParentID == nil || *repo.reorderParentID != "p1" {
			t.Errorf("parent id = %v, want p1", repo.reorderParentID)
		}
		if len(repo.reorderFolderIDs) != 2 || repo.reorderFolderIDs[0] != "a" {
			t.Errorf("ids = %v, want [a b]", repo.reorderFolderIDs)
		}
	})

	t.Run("an empty list is a no-op", func(t *testing.T) {
		repo := &fakeRepo{reorderFoldersErr: errors.New("should not be called")}
		svc := NewService(repo)
		if err := svc.ReorderFolders(context.Background(), nil, nil); err != nil {
			t.Fatalf("empty reorder should be a no-op, got %v", err)
		}
		if repo.reorderFolderIDs != nil {
			t.Errorf("repo should not have been called, got %v", repo.reorderFolderIDs)
		}
	})
}

func ptr(s string) *string { return &s }

func TestServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "valid", input: "folder", wantErr: false},
		{name: "empty", input: "", wantErr: true},
		{name: "whitespace", input: "   ", wantErr: true},
		{name: "too long", input: strings.Repeat("x", maxNameLen+1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(&fakeRepo{})
			_, err := svc.Create(context.Background(), tt.input, nil)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalid) {
					t.Errorf("got %v, want ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestServiceCreateTrimsAndPassesParent(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)

	parent := ptr("parent-id")
	if _, err := svc.Create(context.Background(), "  spaced  ", parent); err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.createName != "spaced" {
		t.Errorf("createName = %q, want trimmed %q", repo.createName, "spaced")
	}
	if repo.createParent != parent {
		t.Errorf("createParent = %v, want %v", repo.createParent, parent)
	}
}

func TestServiceUpdateRejectsBadName(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)

	if _, err := svc.Update(context.Background(), "id", "  ", nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("got %v, want ErrInvalid", err)
	}
	if repo.updateID != "" {
		t.Error("repo.Update should not be called when the name is invalid")
	}
}

func TestServiceUpdateRejectsSelfParent(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)

	_, err := svc.Update(context.Background(), "a", "name", ptr("a"))
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("got %v, want ErrInvalid", err)
	}
	if repo.updateID != "" {
		t.Error("repo.Update should not be called for a self-parent move")
	}
}

// tree fixture: root -> child -> grandchild
func cycleFixture() []Folder {
	return []Folder{
		{ID: "root", ParentID: nil, Name: "root"},
		{ID: "child", ParentID: ptr("root"), Name: "child"},
		{ID: "grandchild", ParentID: ptr("child"), Name: "grandchild"},
	}
}

func TestServiceUpdateRejectsDescendantParent(t *testing.T) {
	repo := &fakeRepo{listFolders: cycleFixture()}
	svc := NewService(repo)

	// Moving root under its own grandchild closes a loop.
	_, err := svc.Update(context.Background(), "root", "root", ptr("grandchild"))
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("got %v, want ErrInvalid", err)
	}
	if repo.updateID != "" {
		t.Error("repo.Update should not be called for a cyclic move")
	}
}

func TestServiceUpdateAllowsValidMove(t *testing.T) {
	repo := &fakeRepo{listFolders: cycleFixture()}
	svc := NewService(repo)

	// Moving grandchild under root is acyclic.
	if _, err := svc.Update(context.Background(), "grandchild", "grandchild", ptr("root")); err != nil {
		t.Fatalf("update: %v", err)
	}
	if repo.updateID != "grandchild" {
		t.Errorf("updateID = %q, want grandchild", repo.updateID)
	}
	if repo.updateParent == nil || *repo.updateParent != "root" {
		t.Errorf("updateParent = %v, want root", repo.updateParent)
	}
}

func TestServiceTree(t *testing.T) {
	repo := &fakeRepo{listFolders: []Folder{
		{ID: "root", ParentID: nil, Name: "root"},
		{ID: "a", ParentID: ptr("root"), Name: "a"},
		{ID: "b", ParentID: ptr("root"), Name: "b"},
		{ID: "a1", ParentID: ptr("a"), Name: "a1"},
		{ID: "lone", ParentID: nil, Name: "lone"},
	}}
	svc := NewService(repo)

	tree, err := svc.Tree(context.Background())
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("roots = %d, want 2", len(tree))
	}
	root := tree[0]
	if root.ID != "root" || len(root.Children) != 2 {
		t.Fatalf("root = %+v, want 2 children", root)
	}
	if root.Children[0].ID != "a" || len(root.Children[0].Children) != 1 {
		t.Errorf("child a = %+v, want 1 grandchild", root.Children[0])
	}
	if root.Children[0].Children[0].ID != "a1" {
		t.Errorf("grandchild = %q, want a1", root.Children[0].Children[0].ID)
	}
	if tree[1].ID != "lone" || len(tree[1].Children) != 0 {
		t.Errorf("second root = %+v, want lone with no children", tree[1])
	}
}

func TestServicePassThroughErrors(t *testing.T) {
	repo := &fakeRepo{
		getErr:     ErrNotFound,
		deleteErr:  ErrNotFound,
		addErr:     ErrNotFound,
		removeErr:  ErrNotFound,
		listIntErr: ErrNotFound,
	}
	svc := NewService(repo)
	ctx := context.Background()

	if _, err := svc.Get(ctx, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get: got %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete: got %v, want ErrNotFound", err)
	}
	if err := svc.AddIntegration(ctx, "f", "i"); !errors.Is(err, ErrNotFound) {
		t.Errorf("add: got %v, want ErrNotFound", err)
	}
	if err := svc.RemoveIntegration(ctx, "f", "i"); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove: got %v, want ErrNotFound", err)
	}
	if _, err := svc.ListIntegrations(ctx, "f"); !errors.Is(err, ErrNotFound) {
		t.Errorf("list integrations: got %v, want ErrNotFound", err)
	}
}
