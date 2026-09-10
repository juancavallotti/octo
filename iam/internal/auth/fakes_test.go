package auth

import (
	"context"
	"fmt"
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
	next      int
}

func newMemUsers() *memUsers {
	return &memUsers{
		byID:      map[string]*user.User{},
		bySubject: map[string]string{},
		roles:     map[string][]user.Role{},
	}
}

func (m *memUsers) Upsert(_ context.Context, subject, email, name string) (user.User, bool, error) {
	if id, ok := m.bySubject[subject]; ok {
		u := m.byID[id]
		u.Email, u.Name, u.LastLoginAt = email, name, time.Now()
		return *u, false, nil
	}
	m.next++
	// Shaped like a UUID, because the exchange puts it in a token's `sub` and a
	// test asserting on that should not be reading something that could not occur.
	id := fmt.Sprintf("00000000-0000-0000-0000-%012d", m.next)
	u := &user.User{
		ID: id, Subject: subject, Email: email, Name: name,
		CreatedAt: time.Now(), LastLoginAt: time.Now(),
	}
	m.byID[id] = u
	m.bySubject[subject] = id
	return *u, true, nil
}

func (m *memUsers) Create(_ context.Context, subject, email, name string) (user.User, error) {
	if _, taken := m.bySubject[subject]; taken {
		return user.User{}, user.ErrConflict
	}
	m.next++
	id := fmt.Sprintf("00000000-0000-0000-0000-%012d", m.next)
	u := &user.User{
		ID: id, Subject: subject, Email: email, Name: name,
		CreatedAt: time.Now(), LastLoginAt: time.Now(),
	}
	m.byID[id] = u
	m.bySubject[subject] = id
	return *u, nil
}

func (m *memUsers) Update(_ context.Context, id, email, name string) error {
	u, ok := m.byID[id]
	if !ok {
		return user.ErrNotFound
	}
	u.Email, u.Name = email, name
	return nil
}

func (m *memUsers) Delete(_ context.Context, id string) error {
	u, ok := m.byID[id]
	if !ok {
		return user.ErrNotFound
	}
	delete(m.bySubject, u.Subject)
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

func (m *memUsers) List(ctx context.Context) ([]user.User, error) {
	out := make([]user.User, 0, len(m.byID))
	for id := range m.byID {
		u, err := m.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
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

func (m *memUsers) Revoke(_ context.Context, userID string, revoked user.Role) error {
	if _, ok := m.byID[userID]; !ok {
		return user.ErrNotFound
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

func (m *memUsers) EnsureFirstAdmin(ctx context.Context, userID string, created bool) (bool, error) {
	if !created {
		return false, nil
	}
	admins, err := m.CountWithRole(ctx, user.RoleAdmin)
	if err != nil || admins > 0 {
		return false, err
	}
	if _, ok := m.byID[userID]; !ok {
		return false, nil
	}
	m.roles[userID] = append(m.roles[userID], user.RoleAdmin)
	return true, nil
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

func (m *memKeys) Verifiers(_ context.Context, now time.Time) ([]signing.Key, error) {
	out := make([]signing.Key, 0, len(m.keys))
	for _, k := range m.keys {
		if k.ExpiresAt.After(now) {
			out = append(out, k)
		}
	}
	return out, nil
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
