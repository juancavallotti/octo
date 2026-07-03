package resource

import (
	"context"
	"errors"
	"testing"
)

// fakeRepo is a hand-written repository for service unit tests: it records the
// arguments it receives and returns canned results.
type fakeRepo struct {
	createIntegrationID string
	createKind          string
	createName          string
	createContent       string
	createErr           error
	createCalled        bool

	listResult []Resource
}

func (f *fakeRepo) Create(_ context.Context, integrationID, kind, name, content string) (Resource, error) {
	f.createCalled = true
	f.createIntegrationID = integrationID
	f.createKind = kind
	f.createName = name
	f.createContent = content
	if f.createErr != nil {
		return Resource{}, f.createErr
	}
	return Resource{ID: "res-1", IntegrationID: integrationID, Kind: kind, Name: name, Content: content}, nil
}

func (f *fakeRepo) Get(_ context.Context, integrationID, id string) (Resource, error) {
	return Resource{ID: id, IntegrationID: integrationID}, nil
}

func (f *fakeRepo) ListByIntegration(_ context.Context, _ string) ([]Resource, error) {
	return f.listResult, nil
}

func (f *fakeRepo) Update(_ context.Context, integrationID, id, kind, name, content string) (Resource, error) {
	return Resource{ID: id, IntegrationID: integrationID, Kind: kind, Name: name, Content: content}, nil
}

func (f *fakeRepo) Delete(_ context.Context, _, _ string) error { return nil }

func TestCreate(t *testing.T) {
	t.Run("trims the name and persists a valid resource", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo)
		res, err := svc.Create(context.Background(), "int-1", KindTemplate, "  templates/welcome.tmpl  ", "hi")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if repo.createName != "templates/welcome.tmpl" {
			t.Errorf("name = %q, want trimmed path", repo.createName)
		}
		if repo.createKind != KindTemplate {
			t.Errorf("kind = %q, want %q", repo.createKind, KindTemplate)
		}
		if res.ID != "res-1" {
			t.Errorf("returned id = %q, want res-1", res.ID)
		}
	})

	t.Run("allows path-like names with internal slashes", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo)
		for _, name := range []string{".env.dev", "templates/mail/welcome.tmpl", "a/b/c.txt"} {
			if _, err := svc.Create(context.Background(), "int-1", KindEnv, name, ""); err != nil {
				t.Errorf("name %q: unexpected error %v", name, err)
			}
		}
	})

	t.Run("rejects an invalid kind before touching the repo", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo)
		if _, err := svc.Create(context.Background(), "int-1", "binary", "x", ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("error = %v, want ErrInvalid", err)
		}
		if repo.createCalled {
			t.Error("repo.Create must not be called for an invalid kind")
		}
	})

	t.Run("rejects an unsafe or empty name", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo)
		for _, name := range []string{"", "   ", "/abs/path", "../escape", "a/../b", "trailing/"} {
			if _, err := svc.Create(context.Background(), "int-1", KindEnv, name, ""); !errors.Is(err, ErrInvalid) {
				t.Errorf("name %q: error = %v, want ErrInvalid", name, err)
			}
		}
		if repo.createCalled {
			t.Error("repo.Create must not be called for an invalid name")
		}
	})

	t.Run("propagates a name-exists conflict from the repo", func(t *testing.T) {
		repo := &fakeRepo{createErr: ErrNameExists}
		svc := NewService(repo)
		if _, err := svc.Create(context.Background(), "int-1", KindEnv, ".env", ""); !errors.Is(err, ErrNameExists) {
			t.Errorf("error = %v, want ErrNameExists", err)
		}
	})
}
