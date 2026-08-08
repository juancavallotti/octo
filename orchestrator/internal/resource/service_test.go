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

// fakeNotifier records the integrations it was told about, and can be made to fail.
type fakeNotifier struct {
	got []string
	err error
}

func (f *fakeNotifier) NotifyIntegrationChanged(_ context.Context, id string) error {
	f.got = append(f.got, id)
	return f.err
}

// TestWritesNotify: a dev run loads resources too, so editing an env file is a change
// to what the running app does. A delete counts — it is what tells the sidecar to prune
// a file whose secrets should stop being on disk.
func TestWritesNotify(t *testing.T) {
	for name, write := range map[string]func(*Service) error{
		"create": func(s *Service) error {
			_, err := s.Create(context.Background(), "int-1", KindEnv, ".env.dev", "A=1")
			return err
		},
		"update": func(s *Service) error {
			_, err := s.Update(context.Background(), "int-1", "res-1", KindEnv, ".env.dev", "A=2")
			return err
		},
		"delete": func(s *Service) error {
			return s.Delete(context.Background(), "int-1", "res-1")
		},
	} {
		notifier := &fakeNotifier{}
		svc := NewService(&fakeRepo{}, WithReloadNotifier(notifier))
		if err := write(svc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(notifier.got) != 1 || notifier.got[0] != "int-1" {
			t.Errorf("%s notified %v, want [int-1]", name, notifier.got)
		}
	}
}

// TestNotifyFailureDoesNotFailTheWrite: same contract as the integration service — a
// resource save must not be reported as failed because a pod was unreachable.
func TestNotifyFailureDoesNotFailTheWrite(t *testing.T) {
	notifier := &fakeNotifier{err: errors.New("sidecar unreachable")}
	svc := NewService(&fakeRepo{}, WithReloadNotifier(notifier))

	if _, err := svc.Create(context.Background(), "int-1", KindEnv, ".env.dev", "A=1"); err != nil {
		t.Fatalf("Create failed because the notification did: %v", err)
	}
}

// TestInvalidWriteDoesNotNotify: validation rejected it, so nothing changed.
func TestInvalidWriteDoesNotNotify(t *testing.T) {
	notifier := &fakeNotifier{}
	svc := NewService(&fakeRepo{}, WithReloadNotifier(notifier))

	if _, err := svc.Create(context.Background(), "int-1", "not-a-kind", ".env.dev", "A=1"); err == nil {
		t.Fatal("Create accepted an invalid kind")
	}
	if len(notifier.got) != 0 {
		t.Fatalf("notified %v after a rejected write, want nothing", notifier.got)
	}
}
