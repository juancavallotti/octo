package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRepo is an in-memory repository stand-in. It records whether it was
// called and the name it received, and returns a preset result/error.
type fakeRepo struct {
	called     bool
	gotName    string
	ret        Integration
	retErr     error
	delErr     error
	nameTaken  bool
	nameErr    error
	gotExclude string
	gotActor   string
}

func (f *fakeRepo) NameExists(_ context.Context, name, excludeID string) (bool, error) {
	f.gotName = name
	f.gotExclude = excludeID
	return f.nameTaken, f.nameErr
}

func (f *fakeRepo) Create(_ context.Context, name, _, actorID string) (Integration, error) {
	f.called = true
	f.gotName = name
	f.gotActor = actorID
	return f.ret, f.retErr
}

func (f *fakeRepo) Update(_ context.Context, _, name, _, actorID string) (Integration, error) {
	f.called = true
	f.gotName = name
	f.gotActor = actorID
	return f.ret, f.retErr
}

func (f *fakeRepo) Get(_ context.Context, _ string) (Integration, error) {
	f.called = true
	return f.ret, f.retErr
}

func (f *fakeRepo) List(_ context.Context) ([]Integration, error) {
	f.called = true
	return []Integration{f.ret}, f.retErr
}

func (f *fakeRepo) Delete(_ context.Context, _ string) error {
	f.called = true
	return f.delErr
}

func TestServiceCreateValidation(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantInvalid bool
		wantCalled  bool
	}{
		{name: "valid", input: "  payments  ", wantInvalid: false, wantCalled: true},
		{name: "empty", input: "", wantInvalid: true, wantCalled: false},
		{name: "whitespace only", input: "   ", wantInvalid: true, wantCalled: false},
		{name: "too long", input: strings.Repeat("a", maxNameLen+1), wantInvalid: true, wantCalled: false},
		{name: "at limit", input: strings.Repeat("a", maxNameLen), wantInvalid: false, wantCalled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{ret: Integration{ID: "id-1"}}
			svc := NewService(repo)

			_, err := svc.Create(context.Background(), tt.input, "body", "")

			if tt.wantInvalid && !errors.Is(err, ErrInvalid) {
				t.Errorf("got err %v, want ErrInvalid", err)
			}
			if !tt.wantInvalid && err != nil {
				t.Errorf("unexpected err: %v", err)
			}
			if repo.called != tt.wantCalled {
				t.Errorf("repo called = %v, want %v", repo.called, tt.wantCalled)
			}
		})
	}
}

func TestServiceCreateTrimsName(t *testing.T) {
	repo := &fakeRepo{ret: Integration{ID: "id-1"}}
	svc := NewService(repo)

	if _, err := svc.Create(context.Background(), "  spaced  ", "body", "user-9"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if repo.gotName != "spaced" {
		t.Errorf("repo received name %q, want trimmed %q", repo.gotName, "spaced")
	}
	if repo.gotActor != "user-9" {
		t.Errorf("repo received actor %q, want %q", repo.gotActor, "user-9")
	}
}

func TestServiceUpdateValidationAndTrim(t *testing.T) {
	repo := &fakeRepo{ret: Integration{ID: "id-1"}}
	svc := NewService(repo)

	if _, err := svc.Update(context.Background(), "id-1", "  ", "body", ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty name: got %v, want ErrInvalid", err)
	}
	if repo.called {
		t.Error("repo should not be called on invalid update")
	}

	if _, err := svc.Update(context.Background(), "id-1", "  renamed  ", "body", "user-3"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if repo.gotName != "renamed" {
		t.Errorf("repo received name %q, want trimmed %q", repo.gotName, "renamed")
	}
	if repo.gotActor != "user-3" {
		t.Errorf("repo received actor %q, want %q", repo.gotActor, "user-3")
	}
}

func TestServiceRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()

	// Create: a taken name is rejected before the row is written.
	repo := &fakeRepo{nameTaken: true}
	svc := NewService(repo)
	if _, err := svc.Create(ctx, "  Payments  ", "body", ""); !errors.Is(err, ErrNameTaken) {
		t.Errorf("create duplicate: got %v, want ErrNameTaken", err)
	}
	if repo.called {
		t.Error("repo.Create should not run when the name is taken")
	}
	if repo.gotName != "Payments" {
		t.Errorf("uniqueness checked with name %q, want trimmed %q", repo.gotName, "Payments")
	}
	if repo.gotExclude != "" {
		t.Errorf("create excludeID = %q, want empty", repo.gotExclude)
	}

	// Update: the check excludes the row being renamed.
	repo = &fakeRepo{nameTaken: true}
	svc = NewService(repo)
	if _, err := svc.Update(ctx, "id-1", "Payments", "body", ""); !errors.Is(err, ErrNameTaken) {
		t.Errorf("update duplicate: got %v, want ErrNameTaken", err)
	}
	if repo.gotExclude != "id-1" {
		t.Errorf("update excludeID = %q, want %q", repo.gotExclude, "id-1")
	}
}

func TestServicePassesThroughRepoErrors(t *testing.T) {
	repo := &fakeRepo{retErr: ErrNotFound, delErr: ErrNotFound}
	svc := NewService(repo)
	ctx := context.Background()

	if _, err := svc.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("get: got %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete: got %v, want ErrNotFound", err)
	}
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

// TestUpdateNotifies: the reload trigger lives at the write, so every writer is
// covered — the editor's save, the integrations list, MCP, an API-key client.
func TestUpdateNotifies(t *testing.T) {
	notifier := &fakeNotifier{}
	svc := NewService(&fakeRepo{ret: Integration{ID: "int-1"}}, WithReloadNotifier(notifier))

	if _, err := svc.Update(context.Background(), "int-1", "orders", "definition", "u1"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(notifier.got) != 1 || notifier.got[0] != "int-1" {
		t.Fatalf("notified %v, want [int-1]", notifier.got)
	}
}

// TestUpdateNotifyFailureDoesNotFailTheWrite is the contract, and it is the whole
// reason the notification is best-effort: a save reported as failed because a pod was
// unreachable is data loss, where a save that succeeded with a reload that did not is
// a stale running app the status surface already reports.
func TestUpdateNotifyFailureDoesNotFailTheWrite(t *testing.T) {
	notifier := &fakeNotifier{err: errors.New("sidecar unreachable")}
	repo := &fakeRepo{ret: Integration{ID: "int-1"}}
	svc := NewService(repo, WithReloadNotifier(notifier))

	got, err := svc.Update(context.Background(), "int-1", "orders", "definition", "u1")
	if err != nil {
		t.Fatalf("Update failed because the notification did: %v", err)
	}
	if got.ID != "int-1" {
		t.Errorf("returned %+v, want the persisted integration", got)
	}
	if !repo.called {
		t.Error("the write did not happen")
	}
}

// TestUpdateDoesNotNotifyOnAFailedWrite: nothing changed, so nothing should reload —
// and a reload here would make a dev run re-pull the unchanged definition for nothing.
func TestUpdateDoesNotNotifyOnAFailedWrite(t *testing.T) {
	notifier := &fakeNotifier{}
	svc := NewService(&fakeRepo{retErr: errors.New("constraint violation")}, WithReloadNotifier(notifier))

	if _, err := svc.Update(context.Background(), "int-1", "orders", "definition", "u1"); err == nil {
		t.Fatal("Update reported success")
	}
	if len(notifier.got) != 0 {
		t.Fatalf("notified %v after a failed write, want nothing", notifier.got)
	}
}

// TestWithoutANotifierNothingBreaks: the option is optional, and a self-hosted install
// with no dev runs must not need it.
func TestWithoutANotifierNothingBreaks(t *testing.T) {
	svc := NewService(&fakeRepo{ret: Integration{ID: "int-1"}})
	if _, err := svc.Update(context.Background(), "int-1", "orders", "definition", "u1"); err != nil {
		t.Fatalf("Update with no notifier: %v", err)
	}
}
