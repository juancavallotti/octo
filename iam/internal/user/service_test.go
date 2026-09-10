package user

import (
	"context"
	"errors"
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
		grantedBy: map[string]*string{},
	}
}

func (m *memRepo) fail() error {
	err := m.failNext
	m.failNext = nil
	return err
}

func (m *memRepo) Upsert(_ context.Context, subject, email, name string) (User, bool, error) {
	if err := m.fail(); err != nil {
		return User{}, false, err
	}
	if id, ok := m.bySubject[subject]; ok {
		u := m.users[id]
		u.Email, u.Name = email, name
		u.LastLoginAt = time.Now()
		return *u, false, nil
	}
	m.nextID++
	id := string(rune('a'+m.nextID-1)) + "0000000-0000-0000-0000-000000000000"
	u := &User{
		ID: id, Subject: subject, Email: email, Name: name,
		CreatedAt: time.Now(), LastLoginAt: time.Now(),
	}
	m.users[id] = u
	m.bySubject[subject] = id
	return *u, true, nil
}

func (m *memRepo) Create(_ context.Context, subject, email, name string) (User, error) {
	if err := m.fail(); err != nil {
		return User{}, err
	}
	if _, taken := m.bySubject[subject]; taken {
		return User{}, ErrConflict
	}
	m.nextID++
	id := string(rune('a'+m.nextID-1)) + "0000000-0000-0000-0000-000000000000"
	u := &User{
		ID: id, Subject: subject, Email: email, Name: name,
		CreatedAt: time.Now(), LastLoginAt: time.Now(),
	}
	m.users[id] = u
	m.bySubject[subject] = id
	return *u, nil
}

func (m *memRepo) Update(_ context.Context, id, email, name string) error {
	if err := m.fail(); err != nil {
		return err
	}
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	u.Email, u.Name = email, name
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
	delete(m.bySubject, u.Subject)
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

func (m *memRepo) List(_ context.Context) ([]User, error) {
	if err := m.fail(); err != nil {
		return nil, err
	}
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, *u)
	}
	return out, nil
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
	var n int
	for _, u := range m.users {
		if u.HasRole(held) {
			n++
		}
	}
	return n, nil
}

func (m *memRepo) EnsureFirstAdmin(_ context.Context, userID string, created bool) (bool, error) {
	if err := m.fail(); err != nil {
		return false, err
	}
	if !created {
		return false, nil
	}
	for _, u := range m.users {
		if u.HasRole(RoleAdmin) {
			return false, nil
		}
	}
	u, ok := m.users[userID]
	if !ok {
		return false, nil
	}
	u.Roles = append(u.Roles, RoleAdmin)
	return true, nil
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
	if _, err := svc.Create(ctx, "sub-2", "second@example.com", "Second"); err != nil {
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
	// Revoked directly through the repository: the service refuses to remove the
	// last admin, which is a different rule and has its own test below.
	if err := repo.Revoke(ctx, u.ID, RoleAdmin); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

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
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Revoke(last admin) error = %v, want ErrInvalid", err)
	}

	// With a second admin in place the same revocation is allowed.
	// Provisioned first: after the first user, this platform is an allowlist and
	// signing in is not by itself a way to get an account.
	if _, err := svc.Create(ctx, "sub-2", "second@example.com", "Second"); err != nil {
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

	u, err := svc.Create(ctx, "provider|abc", "new@example.com", "New Person")
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

func TestCreateRefusesADuplicateSubject(t *testing.T) {
	svc := NewService(newMemRepo())
	ctx := context.Background()

	if _, err := svc.Create(ctx, "provider|abc", "a@example.com", ""); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Quietly rewriting the existing account's email would be a different and much
	// worse thing than refusing.
	_, err := svc.Create(ctx, "provider|abc", "somebody-else@example.com", "")
	if !errors.Is(err, ErrConflict) {
		t.Errorf("Create() error = %v, want ErrConflict", err)
	}
}

func TestCreateRequiresASubjectAndAnEmail(t *testing.T) {
	svc := NewService(newMemRepo())

	for _, tt := range []struct{ name, subject, email string }{
		{"no subject", "", "a@example.com"},
		{"no email", "provider|abc", ""},
		{"blank subject", "   ", "a@example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), tt.subject, tt.email, ""); !errors.Is(err, ErrInvalid) {
				t.Errorf("Create() error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestUpdateCorrectsTheProfile(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo)
	ctx := context.Background()

	created, err := svc.Create(ctx, "provider|abc", "old@example.com", "Old Name")
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
	// The subject keys the row and is what the provider presents; an update must
	// not be a way to point an account at somebody else.
	if u.Subject != "provider|abc" {
		t.Errorf("subject = %q, want it unchanged", u.Subject)
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

	created, err := svc.Create(ctx, "provider|abc", "a@example.com", "")
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

	only, err := svc.Create(ctx, "provider|only", "only@example.com", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Grant(ctx, only.ID, RoleAdmin, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	if err := svc.Delete(ctx, only.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Delete() of the last admin error = %v, want ErrInvalid", err)
	}

	// With a second administrator in place, the first may go.
	second, err := svc.Create(ctx, "provider|second", "second@example.com", "")
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

	admin, err := svc.Create(ctx, "provider|admin", "admin@example.com", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.Grant(ctx, admin.ID, RoleAdmin, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	other, err := svc.Create(ctx, "provider|other", "other@example.com", "")
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
	created, err := svc.Create(ctx, "sub-2", "invited@example.com", "Invited")
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
