package user

import (
	"context"
	"fmt"
	"strings"
)

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake; *Repo
// satisfies it structurally.
type repository interface {
	Upsert(ctx context.Context, subject, email, name string) (User, bool, error)
	Get(ctx context.Context, id string) (User, error)
	GetBySubject(ctx context.Context, subject string) (User, error)
	List(ctx context.Context) ([]User, error)
	Grant(ctx context.Context, userID string, granted Role, grantedBy *string) error
	Revoke(ctx context.Context, userID string, revoked Role) error
	CountWithRole(ctx context.Context, held Role) (int, error)
	EnsureFirstAdmin(ctx context.Context, userID string, created bool) (bool, error)
}

// Service holds user provisioning and role-granting logic.
type Service struct {
	repo repository
}

// NewService returns a Service backed by repo.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// SignIn provisions or refreshes the user identified by subject and returns them
// with their granted roles. It is what the token exchange calls once it has
// verified an identity provider's token: subject and email are required (a
// principal we cannot identify is rejected), name is best-effort.
//
// The first user ever to sign in is made an admin here — see
// Repo.EnsureFirstAdmin for why that is the only moment it can happen, and why
// it cannot happen twice.
func (s *Service) SignIn(ctx context.Context, subject, email, name string) (User, error) {
	subject = strings.TrimSpace(subject)
	email = strings.TrimSpace(email)
	if subject == "" {
		return User{}, fmt.Errorf("%w: subject is required", ErrInvalid)
	}
	if email == "" {
		return User{}, fmt.Errorf("%w: email is required", ErrInvalid)
	}

	u, created, err := s.repo.Upsert(ctx, subject, email, strings.TrimSpace(name))
	if err != nil {
		return User{}, err
	}
	if _, err := s.repo.EnsureFirstAdmin(ctx, u.ID, created); err != nil {
		return User{}, err
	}

	// Read back rather than assembling from what we just wrote: the roles are the
	// point of the read, and a grant made by EnsureFirstAdmin has to be in the
	// token minted from this.
	return s.repo.Get(ctx, u.ID)
}

// Get returns the user by id, with roles.
func (s *Service) Get(ctx context.Context, id string) (User, error) {
	if strings.TrimSpace(id) == "" {
		return User{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	return s.repo.Get(ctx, id)
}

// GetBySubject returns the user by OIDC subject, with roles.
func (s *Service) GetBySubject(ctx context.Context, subject string) (User, error) {
	if strings.TrimSpace(subject) == "" {
		return User{}, fmt.Errorf("%w: subject is required", ErrInvalid)
	}
	return s.repo.GetBySubject(ctx, subject)
}

// List returns every user with their roles.
func (s *Service) List(ctx context.Context) ([]User, error) {
	return s.repo.List(ctx)
}

// Grant gives a user a role from the catalogue, attributed to grantedBy when the
// grantor is known. A role outside the catalogue is refused here rather than
// stored: the column is a varchar, so this check is what constrains it.
func (s *Service) Grant(ctx context.Context, userID string, granted Role, grantedBy *string) error {
	if err := validate(userID, granted); err != nil {
		return err
	}
	return s.repo.Grant(ctx, userID, granted, grantedBy)
}

// Revoke removes a role from a user.
//
// It refuses to remove the last platform:admin. An install with no admin has
// nobody who can grant a role to anyone — including the role that would fix it —
// so this is not a policy but the one state the system cannot recover from. The
// check races with a concurrent revoke of a different admin in principle; two
// people removing the last two admins at the same instant is not a scenario worth
// a lock, and the recovery is a direct row insert either way.
func (s *Service) Revoke(ctx context.Context, userID string, revoked Role) error {
	if err := validate(userID, revoked); err != nil {
		return err
	}
	if revoked == RoleAdmin {
		admins, err := s.repo.CountWithRole(ctx, RoleAdmin)
		if err != nil {
			return err
		}
		if admins <= 1 {
			return fmt.Errorf(
				"%w: this is the last %s, and removing it would leave nobody who can grant it back",
				ErrInvalid, RoleAdmin)
		}
	}
	return s.repo.Revoke(ctx, userID, revoked)
}

// validate rejects the two mistakes a grant or revoke can make in its arguments.
func validate(userID string, r Role) error {
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("%w: user id is required", ErrInvalid)
	}
	if !ValidRole(r) {
		return fmt.Errorf("%w: %q is not a role", ErrInvalid, string(r))
	}
	return nil
}
