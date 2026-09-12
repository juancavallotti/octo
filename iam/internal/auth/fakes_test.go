package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/juancavallotti/octo/iam/internal/signing"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// The two stores the exchange writes through, in memory. They exist so the
// end-to-end test runs everywhere — CI sets no TEST_DATABASE_URL, and the whole
// path a sign-in takes is the last thing that should only be covered on a
// developer's machine.
//
// Each keeps the invariants its real counterpart gets from Postgres, and the
// packages that own those invariants test them against a real database of their
// own. What is under test here is the exchange, not the storage.

// memUsers satisfies the user package's repository interface.
type memUsers struct {
	byID      map[string]*user.User
	bySubject map[string]string
	roles     map[string][]user.Role
	byEmail   map[string]string
	next      int
}

func newMemUsers() *memUsers {
	return &memUsers{
		byID:      map[string]*user.User{},
		bySubject: map[string]string{},
		byEmail:   map[string]string{},
		roles:     map[string][]user.Role{},
	}
}

func (m *memUsers) Admit(ctx context.Context, subject, email, name string) (user.User, bool, error) {
	now := time.Now()
	if id, ok := m.bySubject[subject]; ok {
		u := m.byID[id]
		// The address is unique in the real schema, so a refresh that would land on
		// somebody else's is a conflict rather than a silent overwrite of their
		// index entry.
		if other, taken := m.byEmail[strings.ToLower(email)]; taken && other != id {
			return user.User{}, false, user.ErrConflict
		}
		delete(m.byEmail, strings.ToLower(u.Email))
		u.Email, u.Name, u.LastLoginAt = email, name, &now
		m.byEmail[strings.ToLower(email)] = id
		return m.mustGet(ctx, id), false, nil
	}
	// Adoption: the row an administrator provisioned for this address takes the
	// subject, once.
	if id, ok := m.byEmail[strings.ToLower(email)]; ok {
		u := m.byID[id]
		if u.Subject != "" && u.Subject != subject {
			return user.User{}, false, user.ErrSubjectMismatch
		}
		u.Subject, u.Name, u.LastLoginAt = subject, name, &now
		m.bySubject[subject] = id
		return m.mustGet(ctx, id), false, nil
	}
	// The allowlist: a stranger is admitted only while there is no administrator
	// to have created an account for them.
	if n, _ := m.CountWithRole(ctx, user.RoleAdmin); n > 0 {
		return user.User{}, false, user.ErrNotProvisioned
	}
	u := m.insert(subject, email, name)
	u.LastLoginAt = &now
	m.roles[u.ID] = []user.Role{user.RoleAdmin}
	return m.mustGet(ctx, u.ID), true, nil
}

// insert adds a row and both indexes, which every creating path needs. The id is
// shaped like a UUID, because the exchange puts it in a token's `sub` and a test
// asserting on that should not be reading something that could not occur.
func (m *memUsers) insert(subject, email, name string) *user.User {
	m.next++
	id := fmt.Sprintf("00000000-0000-0000-0000-%012d", m.next)
	u := &user.User{ID: id, Subject: subject, Email: email, Name: name, CreatedAt: time.Now()}
	m.byID[id] = u
	m.byEmail[strings.ToLower(email)] = id
	if subject != "" {
		m.bySubject[subject] = id
	}
	return u
}

// mustGet reads a user back for a caller that has just written them, where a
// miss would be this fake contradicting itself.
func (m *memUsers) mustGet(ctx context.Context, id string) user.User {
	u, err := m.Get(ctx, id)
	if err != nil {
		panic(err)
	}
	return u
}

func (m *memUsers) Create(_ context.Context, email, name string) (user.User, error) {
	if _, taken := m.byEmail[strings.ToLower(email)]; taken {
		return user.User{}, user.ErrConflict
	}
	return *m.insert("", email, name), nil
}

func (m *memUsers) Update(_ context.Context, id, email, name string) error {
	u, ok := m.byID[id]
	if !ok {
		return user.ErrNotFound
	}
	if other, taken := m.byEmail[strings.ToLower(email)]; taken && other != id {
		return user.ErrConflict
	}
	delete(m.byEmail, strings.ToLower(u.Email))
	u.Email, u.Name = email, name
	m.byEmail[strings.ToLower(email)] = id
	return nil
}

func (m *memUsers) Delete(ctx context.Context, id string) error {
	u, ok := m.byID[id]
	if !ok {
		return user.ErrNotFound
	}
	if m.isLastAdmin(ctx, id) {
		return user.ErrLastAdmin
	}
	delete(m.bySubject, u.Subject)
	delete(m.byEmail, strings.ToLower(u.Email))
	delete(m.byID, id)
	delete(m.roles, id)
	return nil
}

func (m *memUsers) Get(_ context.Context, id string) (user.User, error) {
	u, ok := m.byID[id]
	if !ok {
		return user.User{}, user.ErrNotFound
	}
	withRoles := *u
	withRoles.Roles = append([]user.Role(nil), m.roles[id]...)
	return withRoles, nil
}

func (m *memUsers) GetBySubject(ctx context.Context, subject string) (user.User, error) {
	id, ok := m.bySubject[subject]
	if !ok {
		return user.User{}, user.ErrNotFound
	}
	return m.Get(ctx, id)
}

// List is unpaged here: these tests are about the exchange, and nothing in them
// reads a second page.
func (m *memUsers) List(ctx context.Context, _ string, _ user.Role, _ int, _ string) ([]user.User, string, error) {
	out := make([]user.User, 0, len(m.byID))
	for id := range m.byID {
		u, err := m.Get(ctx, id)
		if err != nil {
			return nil, "", err
		}
		out = append(out, u)
	}
	return out, "", nil
}

func (m *memUsers) Grant(_ context.Context, userID string, granted user.Role, _ *string) error {
	if _, ok := m.byID[userID]; !ok {
		return user.ErrNotFound
	}
	for _, held := range m.roles[userID] {
		if held == granted {
			return nil
		}
	}
	m.roles[userID] = append(m.roles[userID], granted)
	return nil
}

func (m *memUsers) Revoke(ctx context.Context, userID string, revoked user.Role) error {
	if _, ok := m.byID[userID]; !ok {
		return user.ErrNotFound
	}
	if revoked == user.RoleAdmin && m.isLastAdmin(ctx, userID) {
		return user.ErrLastAdmin
	}
	kept := make([]user.Role, 0, len(m.roles[userID]))
	for _, held := range m.roles[userID] {
		if held != revoked {
			kept = append(kept, held)
		}
	}
	m.roles[userID] = kept
	return nil
}

func (m *memUsers) CountWithRole(_ context.Context, held user.Role) (int, error) {
	var n int
	for _, granted := range m.roles {
		for _, r := range granted {
			if r == held {
				n++
			}
		}
	}
	return n, nil
}

// isLastAdmin mirrors the real repository's guard: an operation that would leave
// the platform with no administrator is refused.
func (m *memUsers) isLastAdmin(ctx context.Context, id string) bool {
	u, err := m.Get(ctx, id)
	if err != nil || !u.HasRole(user.RoleAdmin) {
		return false
	}
	n, _ := m.CountWithRole(ctx, user.RoleAdmin)
	return n <= 1
}

// memKeys satisfies the signing package's repository interface.
type memKeys struct {
	keys []signing.Key
}

func newMemKeys() *memKeys { return &memKeys{} }

func (m *memKeys) Current(_ context.Context, now time.Time) (signing.Key, error) {
	var (
		best  signing.Key
		found bool
	)
	for _, k := range m.keys {
		if k.RetireAfter.After(now) && (!found || k.RetireAfter.After(best.RetireAfter)) {
			best, found = k, true
		}
	}
	if !found {
		return signing.Key{}, signing.ErrNoKey
	}
	return best, nil
}

func (m *memKeys) Verifiers(_ context.Context, _ time.Time) ([]signing.Key, error) {
	// Every key, however old: a key goes on verifying what it signed for good.
	return append([]signing.Key(nil), m.keys...), nil
}

func (m *memKeys) Rotate(
	ctx context.Context, now time.Time, generate func() (signing.Key, error),
) (signing.Key, error) {
	if existing, err := m.Current(ctx, now); err == nil {
		return existing, nil
	}
	fresh, err := generate()
	if err != nil {
		return signing.Key{}, err
	}
	m.keys = append(m.keys, fresh)
	return fresh, nil
}
