package snapshot

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

// fakeRepo is a hand-written repository for service unit tests: it records the
// arguments it receives and returns canned results.
type fakeRepo struct {
	createIntegrationID string
	createTag           string
	createDefinition    string
	createErr           error

	listResult []Snapshot
}

func (f *fakeRepo) Create(_ context.Context, integrationID, tag, definition string) (Snapshot, error) {
	f.createIntegrationID = integrationID
	f.createTag = tag
	f.createDefinition = definition
	if f.createErr != nil {
		return Snapshot{}, f.createErr
	}
	return Snapshot{ID: "snap-1", IntegrationID: integrationID, Tag: tag, Definition: definition}, nil
}

func (f *fakeRepo) Get(_ context.Context, id string) (Snapshot, error) {
	return Snapshot{ID: id}, nil
}

func (f *fakeRepo) ListByIntegration(_ context.Context, _ string) ([]Snapshot, error) {
	return f.listResult, nil
}

func (f *fakeRepo) Delete(_ context.Context, _ string) error { return nil }

// fakeIntegrations is a stub integration store returning a canned integration.
type fakeIntegrations struct {
	it  integration.Integration
	err error
}

func (f fakeIntegrations) Get(_ context.Context, _ string) (integration.Integration, error) {
	return f.it, f.err
}

func TestCreate(t *testing.T) {
	t.Run("freezes the live definition under a trimmed tag", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo, fakeIntegrations{
			it: integration.Integration{ID: "int-1", Definition: "flow: yaml"},
		})
		s, err := svc.Create(context.Background(), "int-1", "  v1.0  ")
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if repo.createTag != "v1.0" {
			t.Errorf("tag = %q, want v1.0 (trimmed)", repo.createTag)
		}
		if repo.createDefinition != "flow: yaml" {
			t.Errorf("definition = %q, want the live definition", repo.createDefinition)
		}
		if s.Tag != "v1.0" {
			t.Errorf("returned tag = %q, want v1.0", s.Tag)
		}
	})

	t.Run("rejects an invalid tag before touching the integration", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo, fakeIntegrations{err: errors.New("should not be called")})
		for _, tag := range []string{"", "  ", "bad tag", "no/slash"} {
			if _, err := svc.Create(context.Background(), "int-1", tag); !errors.Is(err, ErrInvalid) {
				t.Errorf("tag %q: error = %v, want ErrInvalid", tag, err)
			}
		}
		if repo.createTag != "" {
			t.Errorf("repo should not have been called, got tag %q", repo.createTag)
		}
	})

	t.Run("maps a missing integration to ErrIntegrationNotFound", func(t *testing.T) {
		svc := NewService(&fakeRepo{}, fakeIntegrations{err: integration.ErrNotFound})
		if _, err := svc.Create(context.Background(), "nope", "v1"); !errors.Is(err, ErrIntegrationNotFound) {
			t.Errorf("error = %v, want ErrIntegrationNotFound", err)
		}
	})

	t.Run("propagates a tag-exists conflict from the repo", func(t *testing.T) {
		repo := &fakeRepo{createErr: ErrTagExists}
		svc := NewService(repo, fakeIntegrations{it: integration.Integration{ID: "int-1"}})
		if _, err := svc.Create(context.Background(), "int-1", "v1"); !errors.Is(err, ErrTagExists) {
			t.Errorf("error = %v, want ErrTagExists", err)
		}
	})
}
