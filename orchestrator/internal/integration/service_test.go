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
	called   bool
	gotName  string
	ret      Integration
	retErr   error
	delErr   error
}

func (f *fakeRepo) Create(_ context.Context, name, _ string) (Integration, error) {
	f.called = true
	f.gotName = name
	return f.ret, f.retErr
}

func (f *fakeRepo) Update(_ context.Context, _, name, _ string) (Integration, error) {
	f.called = true
	f.gotName = name
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

			_, err := svc.Create(context.Background(), tt.input, "body")

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

	if _, err := svc.Create(context.Background(), "  spaced  ", "body"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if repo.gotName != "spaced" {
		t.Errorf("repo received name %q, want trimmed %q", repo.gotName, "spaced")
	}
}

func TestServiceUpdateValidationAndTrim(t *testing.T) {
	repo := &fakeRepo{ret: Integration{ID: "id-1"}}
	svc := NewService(repo)

	if _, err := svc.Update(context.Background(), "id-1", "  ", "body"); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty name: got %v, want ErrInvalid", err)
	}
	if repo.called {
		t.Error("repo should not be called on invalid update")
	}

	if _, err := svc.Update(context.Background(), "id-1", "  renamed  ", "body"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if repo.gotName != "renamed" {
		t.Errorf("repo received name %q, want trimmed %q", repo.gotName, "renamed")
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
