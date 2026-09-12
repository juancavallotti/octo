package user

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// memRepo is a hand-written in-memory repository for the service and handler
// tests. It keeps the same invariants the real one gets from Postgres — a grant
// is unique per (user, role), a grant against an unknown user is ErrNotFound —
// so a test that passes here is testing the service's rules and not a fake that
// agrees with whatever it is told.
type memRepo struct {
	users     map[string]*User
	bySubject map[string]string
	byEmail   map[string]string
	nextID    int

	// grantedBy records who attributed each grant, keyed "userID|role". The real
	// column is nullable and a test needs to see what actually landed in it.
	grantedBy map[string]*string

	// failNext, when set, is returned by the next call to any method. It is how
	// the tests reach the paths that only a database fault can produce.
	failNext error
}

func newMemRepo() *memRepo {
	return &memRepo{
		users:     map[string]*User{},
		bySubject: map[string]string{},
		byEmail:   map[string]string{},
		grantedBy: map[string]*string{},
	}
}

func (m *memRepo) fail() error {
	err := m.failNext
	m.failNext = nil
	return err
}

func (m *memRepo) Admit(_ context.Context, subject, email, name string) (User, bool, error) {
	if err := m.fail(); err != nil {
		return User{}, false, err
	}
	now := time.Now()
	if id, ok := m.bySubject[subject]; ok {
		u := m.users[id]
		if other, taken := m.byEmail[lower(email)]; taken && other != id {
			return User{}, false, ErrConflict
		}
		delete(m.byEmail, lower(u.Email))
		u.Email, u.Name, u.LastLoginAt = email, name, &now
		m.byEmail[lower(email)] = id
		return withRoleList(*u), false, nil
	}
	// Adoption: the row an administrator provisioned for this address takes the
	// subject, once.
	if id, ok := m.byEmail[lower(email)]; ok {
		u := m.users[id]
		if u.Subject != "" && u.Subject != subject {
			return User{}, false, ErrSubjectMismatch
		}
		u.Subject, u.Name, u.LastLoginAt = subject, name, &now
		m.bySubject[subject] = id
		return withRoleList(*u), false, nil
	}
	// The allowlist: somebody with no account is admitted only while there is no
	// administrator to have created one for them.
	if m.countWithRole(RoleAdmin) > 0 {
		return User{}, false, ErrNotProvisioned
	}
	u := m.insert(subject, email, name)
	u.LastLoginAt = &now
	u.Roles = []Role{RoleAdmin}
	return withRoleList(*u), true, nil
}

// insert adds a row and both indexes, which every creating path needs.
func (m *memRepo) insert(subject, email, name string) *User {
	m.nextID++
	id := string(rune('a'+m.nextID-1)) + "0000000-0000-0000-0000-000000000000"
	u := &User{ID: id, Subject: subject, Email: email, Name: name, CreatedAt: time.Now()}
	m.users[id] = u
	m.byEmail[lower(email)] = id
	if subject != "" {
		m.bySubject[subject] = id
	}
	return u
}

// lower matches the real schema's case-insensitive unique index on the address.
func lower(email string) string { return strings.ToLower(email) }

// countWithRole is the shared counter behind CountWithRole and the last-
// administrator rule.
func (m *memRepo) countWithRole(held Role) int {
	var n int
	for _, u := range m.users {
		if u.HasRole(held) {
			n++
		}
	}
	return n
}

// isLastAdmin mirrors the real repository's guard: refusing an operation that
// would leave the platform with no administrator.
func (m *memRepo) isLastAdmin(id string) bool {
	u, ok := m.users[id]
	return ok && u.HasRole(RoleAdmin) && m.countWithRole(RoleAdmin) <= 1
}

func (m *memRepo) Create(_ context.Context, email, name string) (User, error) {
	if err := m.fail(); err != nil {
		return User{}, err
	}
	if _, taken := m.byEmail[lower(email)]; taken {
		return User{}, ErrConflict
	}
	return *m.insert("", email, name), nil
}

func (m *memRepo) Update(_ context.Context, id, email, name string) error {
	if err := m.fail(); err != nil {
		return err
	}
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	if other, taken := m.byEmail[lower(email)]; taken && other != id {
		return ErrConflict
	}
	delete(m.byEmail, lower(u.Email))
	u.Email, u.Name = email, name
	m.byEmail[lower(email)] = id
	return nil
}

func (m *memRepo) Delete(_ context.Context, id string) error {
	if err := m.fail(); err != nil {
		return err
	}
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	if m.isLastAdmin(id) {
		return ErrLastAdmin
	}
	delete(m.bySubject, u.Subject)
	delete(m.byEmail, lower(u.Email))
	delete(m.users, id)
	return nil
}

func (m *memRepo) Get(_ context.Context, id string) (User, error) {
	if err := m.fail(); err != nil {
		return User{}, err
	}
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return withRoleList(*u), nil
}

// withRoleList gives a user an empty role list rather than a nil one, which is
// what the real read does: its aggregate COALESCEs to '{}'. Worth mirroring,
// because a caller that never sees nil here is entitled to stop checking for it.
func withRoleList(u User) User {
	if u.Roles == nil {
		u.Roles = []Role{}
	}
	return u
}

func (m *memRepo) GetBySubject(_ context.Context, subject string) (User, error) {
	if err := m.fail(); err != nil {
		return User{}, err
	}
	id, ok := m.bySubject[subject]
	if !ok {
		return User{}, ErrNotFound
	}
	return withRoleList(*m.users[id]), nil
}

// List mirrors the real one: filtered, ordered by creation, and paged by a
// cursor naming the last row of the previous page.
func (m *memRepo) List(_ context.Context, query string, role Role, limit int, cursor string) ([]User, string, error) {
	if err := m.fail(); err != nil {
		return nil, "", err
	}
	after, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}

	all := make([]User, 0, len(m.users))
	for _, u := range m.users {
		if query != "" && !strings.Contains(lower(u.Name+" "+u.Email), lower(query)) {
			continue
		}
		if role != "" && !u.HasRole(role) {
			continue
		}
		all = append(all, withRoleList(*u))
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.Before(all[j].CreatedAt)
		}
		return all[i].ID < all[j].ID
	})
	if after.id != nil {
		for i, u := range all {
			if u.ID == *after.id {
				all = all[i+1:]
				break
			}
		}
	}
	if len(all) > limit {
		return all[:limit], encodeCursor(all[limit-1].CreatedAt, all[limit-1].ID), nil
	}
	return all, "", nil
}

func (m *memRepo) Grant(_ context.Context, userID string, granted Role, grantedBy *string) error {
	if err := m.fail(); err != nil {
		return err
	}
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	m.grantedBy[userID+"|"+string(granted)] = grantedBy
	if u.HasRole(granted) {
		return nil
	}
	u.Roles = append(u.Roles, granted)
	return nil
}

func (m *memRepo) Revoke(_ context.Context, userID string, revoked Role) error {
	if err := m.fail(); err != nil {
		return err
	}
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	if revoked == RoleAdmin && m.isLastAdmin(userID) {
		return ErrLastAdmin
	}
	kept := u.Roles[:0]
	for _, r := range u.Roles {
		if r != revoked {
			kept = append(kept, r)
		}
	}
	u.Roles = kept
	return nil
}

func (m *memRepo) CountWithRole(_ context.Context, held Role) (int, error) {
	if err := m.fail(); err != nil {
		return 0, err
	}
	return m.countWithRole(held), nil
}

func newService() (*Service, *memRepo) {
	repo := newMemRepo()
	return NewService(repo), repo
}

// The install has to end up with someone who can grant roles, and the only moment
// that can happen without a human is the very first sign-in.
func TestSignInMakesTheFirstUserAnAdmin(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()

	first, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn(first): %v", err)
	}
	if !first.HasRole(RoleAdmin) {
		t.Errorf("first user roles = %v, want to include %q", first.Roles, RoleAdmin)
	}

	// Provisioned first: after the first user, this platform is an allowlist and
	// signing in is not by itself a way to get an account.
	if _, err := svc.Create(ctx, "second@example.com", "Second", nil, nil); err != nil {
		t.Fatalf("Create(second): %v", err)
	}
	second, err := svc.SignIn(ctx, "sub-2", "second@example.com", "Second")
	if err != nil {
		t.Fatalf("SignIn(second): %v", err)
	}
	if len(second.Roles) != 0 {
		t.Errorf("second user roles = %v, want none", second.Roles)
	}
}

// Signing in again must not re-run the bootstrap, or an admin who was
// deliberately demoted on a single-user install could restore themselves by
// logging out and back in.
func TestSignInDoesNotRegrantAdminOnALaterSignIn(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()

	u, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	// Stripped behind the repository's back. Both Revoke and Delete now refuse to
	// remove the last administrator, so this state is not reachable through any
	// supported route — but it is reachable through the database, and what is
	// under test is that a later sign-in does not undo it.
	repo.users[u.ID].Roles = nil

	again, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn(again): %v", err)
	}
	if again.HasRole(RoleAdmin) {
		t.Errorf("roles = %v after signing in again, want the revocation to hold", again.Roles)
	}
}

func TestSignInRefreshesAnExistingUser(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()

	first, err := svc.SignIn(ctx, "sub-1", "old@example.com", "Old Name")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	again, err := svc.SignIn(ctx, "sub-1", "new@example.com", "New Name")
	if err != nil {
		t.Fatalf("SignIn(again): %v", err)
	}

	if again.ID != first.ID {
		t.Errorf("id = %q on the second sign-in, want the stable %q", again.ID, first.ID)
	}
	if again.Email != "new@example.com" || again.Name != "New Name" {
		t.Errorf("got %q/%q, want the identity provider's current email and name",
			again.Email, again.Name)
	}
}

func TestSignInRejectsAnUnidentifiablePrincipal(t *testing.T) {
	tests := []struct {
		name    string
		subject string
		email   string
	}{
		{"no subject", "", "someone@example.com"},
		{"blank subject", "   ", "someone@example.com"},
		{"no email", "sub-1", ""},
		{"blank email", "sub-1", "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newService()
			_, err := svc.SignIn(context.Background(), tt.subject, tt.email, "Name")
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("SignIn() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestGrantRejectsARoleOutsideTheCatalogue(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	u, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	err = svc.Grant(ctx, u.ID, Role("platform:superuser"), nil)
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("Grant(unknown role) error = %v, want ErrInvalid", err)
	}
}

func TestGrantIsIdempotent(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	u, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	for range 2 {
		if err := svc.Grant(ctx, u.ID, RoleOperator, nil); err != nil {
			t.Fatalf("Grant: %v", err)
		}
	}

	got, err := svc.Get(ctx, u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	var operators int
	for _, r := range got.Roles {
		if r == RoleOperator {
			operators++
		}
	}
	if operators != 1 {
		t.Errorf("granted %s %d times, want it held once", RoleOperator, operators)
	}
}

func TestGrantAgainstAnUnknownUserIsNotFound(t *testing.T) {
	svc, _ := newService()
	err := svc.Grant(context.Background(), "nobody", RoleMonitor, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Grant(unknown user) error = %v, want ErrNotFound", err)
	}
}

// An install with no admin has nobody who can grant the role back, so this is
// the one state the system cannot recover from on its own.
func TestRevokeRefusesToRemoveTheLastAdmin(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	first, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	err = svc.Revoke(ctx, first.ID, RoleAdmin)
	if !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("Revoke(last admin) error = %v, want ErrLastAdmin", err)
	}

	// With a second admin in place the same revocation is allowed.
	// Provisioned first: after the first user, this platform is an allowlist and
	// signing in is not by itself a way to get an account.
	if _, err := svc.Create(ctx, "second@example.com", "Second", nil, nil); err != nil {
		t.Fatalf("Create(second): %v", err)
	}
	second, err := svc.SignIn(ctx, "sub-2", "second@example.com", "Second")
	if err != nil {
		t.Fatalf("SignIn(second): %v", err)
	}
	if err := svc.Grant(ctx, second.ID, RoleAdmin, &first.ID); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := svc.Revoke(ctx, first.ID, RoleAdmin); err != nil {
		t.Errorf("Revoke with two admins: %v", err)
	}
}

func TestRevokeANonAdminRoleIsNotGatedOnTheAdminCount(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()
	u, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if err := svc.Grant(ctx, u.ID, RoleMonitor, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	// The only admin on the install, revoking something that is not admin.
	if err := svc.Revoke(ctx, u.ID, RoleMonitor); err != nil {
		t.Errorf("Revoke(monitor): %v", err)
	}
}

func TestGetAndGrantRejectABlankUserID(t *testing.T) {
	svc, _ := newService()
	ctx := context.Background()

	if _, err := svc.Get(ctx, "  "); !errors.Is(err, ErrInvalid) {
		t.Errorf("Get(blank) error = %v, want ErrInvalid", err)
	}
	if err := svc.Grant(ctx, "", RoleMonitor, nil); !errors.Is(err, ErrInvalid) {
		t.Errorf("Grant(blank) error = %v, want ErrInvalid", err)
	}
	if _, err := svc.GetBySubject(ctx, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("GetBySubject(blank) error = %v, want ErrInvalid", err)
	}
}

// A fault reading the admin count must not be read as "there are none", which
// would turn a transient database error into permission to remove the last one.
func TestRevokeSurfacesACountFailure(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()
	u, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	boom := errors.New("connection reset")
	repo.failNext = boom
	if err := svc.Revoke(ctx, u.ID, RoleAdmin); !errors.Is(err, boom) {
		t.Errorf("Revoke() error = %v, want the repository's error", err)
	}
}

func TestCreateProvisionsAUserWhoHasNeverSignedIn(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	u, err := svc.Create(ctx, "new@example.com", "New Person", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.Email != "new@example.com" || u.Name != "New Person" {
		t.Errorf("Create() = %+v, want the profile it was given", u)
	}
	// Read back with roles, so a created user is shaped like a listed one.
	if u.Roles == nil {
		t.Error("a created user reports null roles rather than an empty list")
	}
}

func TestCreateRefusesAnAddressThatAlreadyHasAnAccount(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	if _, err := svc.Create(ctx, "a@example.com", "First", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Quietly rewriting the existing account's name would be a different and much
	// worse thing than refusing.
	_, err := svc.Create(ctx, "a@example.com", "Somebody Else", nil, nil)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("Create() error = %v, want ErrConflict", err)
	}
}

// The case a lower(email) index exists for: providers treat addresses
// case-insensitively, so two rows differing only in case would make "who is this
// address" unanswerable at exactly the moment it decides who somebody is.
func TestCreateRefusesTheSameAddressInDifferentCase(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	if _, err := svc.Create(ctx, "Ada@Example.com", "Ada", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Create(ctx, "ada@example.com", "Ada", nil, nil); !errors.Is(err, ErrConflict) {
		t.Errorf("Create() error = %v, want ErrConflict", err)
	}
}

// What an administrator types is an address, so what is checked is that it
// could be one. Nothing stricter: whether it exists is the identity provider's
// answer, not ours.
func TestCreateRefusesWhatCannotBeAnAddress(t *testing.T) {
	svc := NewService(newMemRepo())

	for _, tt := range []struct{ name, email string }{
		{"empty", ""},
		{"blank", "   "},
		{"a name typed into the address box", "Ada Lovelace"},
		{"no local part", "@example.com"},
		{"no domain", "ada@"},
		{"two at signs", "ada@example@com"},
		{"a space inside", "ada @example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), tt.email, "", nil, nil); !errors.Is(err, ErrInvalid) {
				t.Errorf("Create() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestUpdateCorrectsTheProfile(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "old@example.com", "Old Name", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	u, err := svc.Update(ctx, created.ID, "new@example.com", "New Name")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if u.Email != "new@example.com" || u.Name != "New Name" {
		t.Errorf("Update() = %+v, want the new profile", u)
	}
	// A provisioned person has no subject until they arrive, and correcting their
	// profile must not be a way to invent one.
	if u.Subject != "" {
		t.Errorf("subject = %q, want it still unset", u.Subject)
	}
}

func TestUpdateAndDeleteReportAnUnknownUser(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	if _, err := svc.Update(ctx, "nobody", "a@example.com", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update() error = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteRemovesAUser(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	created, err := svc.Create(ctx, "a@example.com", "", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after Delete error = %v, want ErrNotFound", err)
	}
}

// Without this rule an administrator could delete every account including their
// own, and the next person to sign in would look like the first user of a fresh
// install and be made an admin by the bootstrap — so "delete everyone" would
// hand the platform to whoever knocks next.
func TestDeleteRefusesTheLastAdmin(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	only, err := svc.Create(ctx, "only@example.com", "", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Grant(ctx, only.ID, RoleAdmin, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	if err := svc.Delete(ctx, only.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("Delete() of the last admin error = %v, want ErrLastAdmin", err)
	}

	// With a second administrator in place, the first may go.
	second, err := svc.Create(ctx, "second@example.com", "", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Grant(ctx, second.ID, RoleAdmin, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := svc.Delete(ctx, only.ID); err != nil {
		t.Errorf("Delete() with another admin present: %v", err)
	}
}

// Somebody holding no admin role is not the last administrator, however few
// users are left.
func TestDeleteAllowsRemovingTheLastNonAdmin(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	admin, err := svc.Create(ctx, "admin@example.com", "", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Grant(ctx, admin.ID, RoleAdmin, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	other, err := svc.Create(ctx, "other@example.com", "", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Delete(ctx, other.ID); err != nil {
		t.Errorf("Delete() of a non-admin: %v", err)
	}
}

// The allowlist. Being able to authenticate at the identity provider is not by
// itself permission to be here — an installation whose provider admits a whole
// company should not admit a whole company.
func TestSignInRefusesSomebodyWithNoAccount(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	// The first sign-in is the exception: nobody could have created that account.
	if _, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First"); err != nil {
		t.Fatalf("SignIn(first): %v", err)
	}

	_, err := svc.SignIn(ctx, "sub-2", "stranger@example.com", "Stranger")
	if !errors.Is(err, ErrNotProvisioned) {
		t.Errorf("SignIn() of an unprovisioned caller error = %v, want ErrNotProvisioned", err)
	}
}

func TestSignInAdmitsSomebodyAnAdministratorCreated(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	if _, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First"); err != nil {
		t.Fatalf("SignIn(first): %v", err)
	}
	created, err := svc.Create(ctx, "invited@example.com", "Invited", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	u, err := svc.SignIn(ctx, "sub-2", "invited@example.com", "Invited Person")
	if err != nil {
		t.Fatalf("SignIn(provisioned): %v", err)
	}
	if u.ID != created.ID {
		t.Errorf("sign-in produced user %q, want the created %q", u.ID, created.ID)
	}
	// The provider is authoritative for the profile, so a sign-in refreshes it.
	if u.Name != "Invited Person" {
		t.Errorf("name = %q, want the provider's", u.Name)
	}
}

// The bootstrap exception is keyed on there being no administrator, not on the
// table being empty: an installation whose only user has been stripped of admin
// is still one nobody can administer.
func TestSignInBootstrapsWhileNobodyIsAnAdministrator(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	first, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn(first): %v", err)
	}
	if !first.HasRole(RoleAdmin) {
		t.Fatalf("the first user is not an admin: %v", first.Roles)
	}

	// Strip the grant behind the service's back, standing in for a database that
	// has lost its administrators somehow.
	repo.users[first.ID].Roles = nil

	second, err := svc.SignIn(ctx, "sub-2", "second@example.com", "Second")
	if err != nil {
		t.Fatalf("SignIn(second) with no admin present: %v", err)
	}
	if !second.HasRole(RoleAdmin) {
		t.Errorf("second user roles = %v, want the bootstrap to have made them an admin", second.Roles)
	}
}

// The provisioning path this platform is built on: an administrator names an
// address, and the person who turns up with it takes the row.
func TestASignInAdoptsTheRowProvisionedForThatAddress(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	// Somebody has to be the administrator first, or the newcomer path would
	// admit the second person as the first.
	if _, err := svc.SignIn(ctx, "sub-admin", "admin@example.com", "Admin"); err != nil {
		t.Fatalf("SignIn(admin): %v", err)
	}
	provisioned, err := svc.Create(ctx, "ada@example.com", "Ada Lovelace", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if provisioned.Subject != "" || provisioned.LastLoginAt != nil {
		t.Fatalf("a provisioned user arrived with a subject or a login: %+v", provisioned)
	}

	arrived, err := svc.SignIn(ctx, "provider|ada", "ada@example.com", "Ada")
	if err != nil {
		t.Fatalf("SignIn(ada): %v", err)
	}
	if arrived.ID != provisioned.ID {
		t.Errorf("id = %q, want the provisioned row %q", arrived.ID, provisioned.ID)
	}
	if arrived.Subject != "provider|ada" {
		t.Errorf("subject = %q, want the one the provider presented", arrived.Subject)
	}
	if arrived.LastLoginAt == nil {
		t.Error("the adopted row reports no sign-in")
	}
}

// The address decides once. After that the subject is what the row is keyed by,
// and a second principal presenting the same address is a different person
// wearing a familiar name.
func TestASecondPrincipalCannotTakeAnAdoptedRow(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	if _, err := svc.SignIn(ctx, "sub-admin", "admin@example.com", "Admin"); err != nil {
		t.Fatalf("SignIn(admin): %v", err)
	}
	if _, err := svc.Create(ctx, "ada@example.com", "Ada", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.SignIn(ctx, "provider|ada", "ada@example.com", "Ada"); err != nil {
		t.Fatalf("SignIn(ada): %v", err)
	}

	_, err := svc.SignIn(ctx, "provider|impostor", "ada@example.com", "Ada")
	if !errors.Is(err, ErrSubjectMismatch) {
		t.Errorf("SignIn() error = %v, want ErrSubjectMismatch", err)
	}
}

// The directory is paged because an administrator reads it a screenful at a
// time, and a cursor rather than an offset because rows are added and removed
// while they read.
func TestListPagesThroughTheDirectoryWithoutRepeatingOrSkipping(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	for i := range 5 {
		if _, err := svc.Create(ctx, fmt.Sprintf("person-%d@example.com", i), "", nil, nil); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		if page > 5 {
			t.Fatal("the listing never reported a last page")
		}
		users, next, err := svc.List(ctx, "", "", 2, cursor)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, u := range users {
			if seen[u.ID] {
				t.Errorf("%s appeared on two pages", u.Email)
			}
			seen[u.ID] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 5 {
		t.Errorf("saw %d of 5 people", len(seen))
	}
}

// The filter is the server's, so a page is a page of matches rather than a page
// of everybody with the non-matches removed.
func TestListFiltersOnNameAndAddress(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	for _, p := range [][2]string{
		{"ada@example.com", "Ada Lovelace"},
		{"grace@example.com", "Grace Hopper"},
		{"alan@elsewhere.test", "Alan Turing"},
	} {
		if _, err := svc.Create(ctx, p[0], p[1], nil, nil); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	for _, tt := range []struct {
		query string
		want  int
	}{
		{"lovelace", 1},
		{"example.com", 2},
		{"a", 3},
		{"nobody", 0},
		// A wildcard is a character somebody typed, not a pattern.
		{"%", 0},
	} {
		t.Run(tt.query, func(t *testing.T) {
			users, _, err := svc.List(ctx, tt.query, "", 25, "")
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(users) != tt.want {
				t.Errorf("List(%q) returned %d, want %d", tt.query, len(users), tt.want)
			}
		})
	}
}

// A cursor is this module's bookkeeping, and one a caller composed by hand is a
// mistake worth reporting rather than a listing that silently starts over.
func TestAnUnreadableCursorIsRefused(t *testing.T) {
	svc := NewService(newMemRepo())

	if _, _, err := svc.List(context.Background(), "", "", 25, "not-a-cursor"); !errors.Is(err, ErrInvalid) {
		t.Errorf("List() error = %v, want ErrInvalid", err)
	}
}

// The role filter has to be the server's too, or a page is a page of everybody
// with the non-matches taken out of it.
func TestListFiltersByRole(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	for _, address := range []string{"a@example.com", "b@example.com"} {
		if _, err := svc.Create(ctx, address, "", nil, nil); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	people, _, err := svc.List(ctx, "", "", 25, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := svc.Grant(ctx, people[0].ID, RoleMonitor, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	monitors, _, err := svc.List(ctx, "", RoleMonitor, 25, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(monitors) != 1 || monitors[0].ID != people[0].ID {
		t.Errorf("List(monitor) = %+v, want the one person holding it", monitors)
	}
}

// A role that is not in the catalogue is the caller's mistake, and answering
// "nobody holds that" would read as a fact about the directory.
func TestListRefusesARoleOutsideTheCatalogue(t *testing.T) {
	svc := NewService(newMemRepo())

	if _, _, err := svc.List(context.Background(), "", "platform:wizard", 25, ""); !errors.Is(err, ErrInvalid) {
		t.Errorf("List() error = %v, want ErrInvalid", err)
	}
}

// A cursor a caller composed by hand carries an id that is not a UUID, which the
// column it is compared against cannot parse. Refused here rather than reaching
// Postgres, where it would come back a 500 for a value the caller supplied.
func TestACursorNamingSomethingThatIsNotAUserIsRefused(t *testing.T) {
	svc := NewService(newMemRepo())
	handmade := base64.RawURLEncoding.EncodeToString([]byte("2026-01-01T00:00:00Z|abc"))

	if _, _, err := svc.List(context.Background(), "", "", 25, handmade); !errors.Is(err, ErrInvalid) {
		t.Errorf("List() error = %v, want ErrInvalid", err)
	}
}

// Letting somebody in and saying what they may do is one decision, so it is one
// call: a person created with roles has them before anybody sees the row.
func TestCreateGrantsTheRolesItWasGiven(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	granter := "admin-id"

	u, err := svc.Create(context.Background(), "ada@example.com", "Ada",
		[]Role{RoleDeveloper, RoleMonitor}, &granter)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !u.HasRole(RoleDeveloper) || !u.HasRole(RoleMonitor) {
		t.Errorf("roles = %v, want both of them", u.Roles)
	}
	if got := repo.grantedBy[u.ID+"|"+string(RoleDeveloper)]; got == nil || *got != granter {
		t.Errorf("granted_by = %v, want the administrator who added them", got)
	}
}

// Validated before the row is written, so the one failure a caller can cause
// cannot leave a person created with half of what was asked for.
func TestCreateWithARoleOutsideTheCatalogueCreatesNobody(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "ada@example.com", "", []Role{"platform:wizard"}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Create() error = %v, want ErrInvalid", err)
	}
	people, _, err := svc.List(ctx, "", "", 25, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(people) != 0 {
		t.Errorf("the refused create left %d people behind", len(people))
	}
}
