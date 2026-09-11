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
	Admit(ctx context.Context, subject, email, name string) (User, bool, error)
	Create(ctx context.Context, email, name string) (User, error)
	Update(ctx context.Context, id, email, name string) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (User, error)
	GetBySubject(ctx context.Context, subject string) (User, error)
	List(ctx context.Context, query string, role Role, limit int, cursor string) ([]User, string, error)
	Grant(ctx context.Context, userID string, granted Role, grantedBy *string) error
	Revoke(ctx context.Context, userID string, revoked Role) error
	CountWithRole(ctx context.Context, held Role) (int, error)
}

// Service holds user provisioning and role-granting logic.
type Service struct {
	repo repository
}

// NewService returns a Service backed by repo.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// SignIn refreshes the user identified by subject and returns them with their
// granted roles. It is what the token exchange calls once it has verified an
// identity provider's token: subject and email are required (a principal we
// cannot identify is rejected), name is best-effort.
//
// **Only the first user is provisioned here.** Everybody after them has to
// already have an account, which an administrator creates — this platform is an
// allowlist, and being able to authenticate at the identity provider is not by
// itself permission to be here. That distinction is the point: the provider says
// who somebody is, and this platform says who may come in, and an installation
// whose provider admits an entire company should not admit an entire company.
//
// The exception is the first ever sign-in, because there is nobody to have
// created that account. See Repo.admitNewcomer for why that moment is
// identifiable and why it cannot happen twice.
//
// Nothing here reads a claim beyond the three arguments. What an address is worth
// as an identity is the identity provider's business: this service takes the
// address it was given, and an installation whose provider hands out addresses
// that are not their owners' has a provider to fix. See Repo.adopt for the one
// moment an address decides anything, and for the two rules that bound it.
func (s *Service) SignIn(ctx context.Context, subject, email, name string) (User, error) {
	subject = strings.TrimSpace(subject)
	email = strings.TrimSpace(email)
	if subject == "" {
		return User{}, fmt.Errorf("%w: subject is required", ErrInvalid)
	}
	if email == "" {
		return User{}, fmt.Errorf("%w: email is required", ErrInvalid)
	}

	u, _, err := s.repo.Admit(ctx, subject, email, strings.TrimSpace(name))
	return u, err
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

// defaultPageSize and maxPageSize bound a listing. The default is a screenful;
// the cap is what stops a caller asking for the whole directory in one request
// and is generous enough that no honest client meets it.
const (
	defaultPageSize = 25
	maxPageSize     = 100
)

// List returns one page of users with their roles, and the cursor for the next
// page or "" on the last.
//
// A limit of zero or less takes the default, and one above the cap is clamped
// rather than refused: the caller asked for as many as possible, and answering
// with the most this service will give is more useful than a 400.
//
// A role outside the catalogue is refused rather than silently ignored: a
// listing filtered by a role that cannot exist would answer "nobody here holds
// that", which reads as an answer about the directory instead of about the
// request.
func (s *Service) List(
	ctx context.Context, query string, role Role, limit int, cursor string,
) ([]User, string, error) {
	if role != "" && !ValidRole(role) {
		return nil, "", fmt.Errorf("%w: %q is not a role", ErrInvalid, string(role))
	}
	switch {
	case limit <= 0:
		limit = defaultPageSize
	case limit > maxPageSize:
		limit = maxPageSize
	}
	return s.repo.List(ctx, strings.TrimSpace(query), role, limit, cursor)
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
	// The last-administrator rule is the repository's, because only it can check
	// and write under one lock — see Repo.Revoke.
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

// Create provisions a user an administrator named, by address, holding roles.
// See Repo.Create for why the OIDC subject is discovered rather than supplied.
//
// The roles come with the person because that is one intent — "let this
// colleague in as an operator" — and splitting it across two calls would leave
// the caller to decide what a half-done one means. Every role is validated
// before the row is written, so the one failure a caller can cause cannot land
// halfway; what remains is a database fault between two statements, which leaves
// the person created with fewer roles and is reported as itself.
//
// `grantedBy` attributes the grants, and is nil when the grantor is not known.
func (s *Service) Create(
	ctx context.Context, email, name string, roles []Role, grantedBy *string,
) (User, error) {
	email = strings.TrimSpace(email)
	if err := validAddress(email); err != nil {
		return User{}, err
	}
	for _, r := range roles {
		if !ValidRole(r) {
			return User{}, fmt.Errorf("%w: %q is not a role", ErrInvalid, string(r))
		}
	}

	u, err := s.repo.Create(ctx, email, strings.TrimSpace(name))
	if err != nil {
		return User{}, err
	}
	for _, r := range roles {
		if err := s.repo.Grant(ctx, u.ID, r, grantedBy); err != nil {
			return User{}, fmt.Errorf("%s was created but could not be granted %s: %w",
				u.Email, string(r), err)
		}
	}
	// Read back rather than returning what the insert gave us, so a created user
	// and a listed one are the same shape — with the roles that landed.
	return s.repo.Get(ctx, u.ID)
}

// Update corrects a user's profile. Only email and name: see Repo.Update.
func (s *Service) Update(ctx context.Context, id, email, name string) (User, error) {
	if strings.TrimSpace(id) == "" {
		return User{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	email = strings.TrimSpace(email)
	if err := validAddress(email); err != nil {
		return User{}, err
	}
	if err := s.repo.Update(ctx, id, email, strings.TrimSpace(name)); err != nil {
		return User{}, err
	}
	return s.repo.Get(ctx, id)
}

// Delete removes a user, refusing to remove the last administrator.
//
// The same rule Revoke enforces, and it matters more here. Without it an
// administrator could delete every account including their own, and the next
// person to sign in would be the first user of what looks like a fresh install
// and be made an admin by the bootstrap — so "delete everyone" would be a way to
// hand the platform to whoever knocks next.
func (s *Service) Delete(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalid)
	}
	// Same rule as Revoke and enforced in the same place, for the same reason.
	return s.repo.Delete(ctx, id)
}

// validAddress rejects what cannot be an address at all.
//
// One @ with something either side, and no spaces. Deliberately not a grammar
// from the RFC: the authority on whether an address exists is the identity
// provider that authenticates it, and a stricter rule here would refuse valid
// addresses while catching nothing an administrator would actually type. What it
// does catch is the two mistakes they do make — an empty field, and a name typed
// into the address box.
func validAddress(email string) error {
	if email == "" {
		return fmt.Errorf("%w: an email address is required", ErrInvalid)
	}
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.ContainsAny(email, " \t") ||
		strings.Contains(domain, "@") {
		return fmt.Errorf("%w: %q is not an email address", ErrInvalid, email)
	}
	return nil
}
