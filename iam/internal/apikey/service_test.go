package apikey

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/iam/internal/user"
)

// fakeRepo is an in-memory store keyed by both id and hash. It records the hash it
// was given at insert so a test can confirm the plaintext never reaches storage.
type fakeRepo struct {
	byID      map[string]APIKey
	byHash    map[string]string // hash -> id
	seq       int
	insertErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[string]APIKey{}, byHash: map[string]string{}}
}

func (f *fakeRepo) Insert(_ context.Context, k APIKey, keyHash string) (APIKey, error) {
	if f.insertErr != nil {
		return APIKey{}, f.insertErr
	}
	f.seq++
	k.ID = string(rune('a' + f.seq))
	k.CreatedAt = time.Unix(int64(f.seq), 0)
	f.byID[k.ID] = k
	f.byHash[keyHash] = k.ID
	return k, nil
}

func (f *fakeRepo) ListByUser(_ context.Context, userID string) ([]APIKey, error) {
	var out []APIKey
	for _, k := range f.byID {
		if k.UserID == userID && k.RevokedAt == nil {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetByHash(_ context.Context, keyHash string) (APIKey, error) {
	id, ok := f.byHash[keyHash]
	if !ok {
		return APIKey{}, ErrNotFound
	}
	k := f.byID[id]
	if k.RevokedAt != nil {
		return APIKey{}, ErrNotFound
	}
	return k, nil
}

func (f *fakeRepo) Revoke(_ context.Context, id, userID string) error {
	k, ok := f.byID[id]
	if !ok || k.UserID != userID || k.RevokedAt != nil {
		return ErrNotFound
	}
	now := time.Unix(99, 0)
	k.RevokedAt = &now
	f.byID[id] = k
	return nil
}

func (f *fakeRepo) TouchLastUsed(_ context.Context, id string) error {
	k, ok := f.byID[id]
	if !ok {
		return ErrNotFound
	}
	now := time.Unix(100, 0)
	k.LastUsedAt = &now
	f.byID[id] = k
	return nil
}

// developer is an owner holding platform:developer, and nothing else.
func developer(id string) user.User {
	return user.User{ID: id, Roles: []user.Role{user.RoleDeveloper}}
}

// create mints a developer-scoped key for a developer owner, expiring in an hour.
func create(t *testing.T, svc *Service, ownerID, name string) (APIKey, string) {
	t.Helper()
	k, token, err := svc.Create(context.Background(), developer(ownerID), name,
		[]user.Role{user.RoleDeveloper}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return k, token
}

func TestCreateReturnsRecognizableTokenAndStoresOnlyHash(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)

	k, token := create(t, svc, "user-1", "ci")
	if !strings.HasPrefix(token, tokenPrefix) {
		t.Errorf("token %q does not start with %q", token, tokenPrefix)
	}
	if k.Prefix == "" || k.Last4 == "" {
		t.Errorf("missing display fragments: %+v", k)
	}
	// The plaintext must not be derivable from what was stored: only its hash is in
	// the repo, and the hash differs from the token.
	if _, ok := repo.byHash[token]; ok {
		t.Error("repo is keyed by plaintext token, not its hash")
	}
	if _, ok := repo.byHash[hashToken(token)]; !ok {
		t.Error("stored hash does not match hashToken(plaintext)")
	}
	if k.ExpiresAt.Before(time.Now()) {
		t.Error("expiry should be in the future")
	}
}

func TestCreateRejectsBadNameAndExpiry(t *testing.T) {
	svc := NewService(newFakeRepo())
	roles := []user.Role{user.RoleDeveloper}
	tests := []struct {
		name      string
		keyName   string
		expiresAt time.Time
		want      error
	}{
		{"empty name", "  ", time.Now().Add(time.Hour), ErrInvalidName},
		{"in the past", "ok", time.Now().Add(-time.Minute), ErrInvalidExpiry},
		{"beyond the cap", "ok", time.Now().Add(maxTTL + time.Hour), ErrInvalidExpiry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.Create(context.Background(), developer("u"), tt.keyName, roles, tt.expiresAt)
			if !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCreateScopesRolesToWhatTheOwnerHolds(t *testing.T) {
	admin := user.User{ID: "a", Roles: []user.Role{user.RoleAdmin}}
	tests := []struct {
		name  string
		owner user.User
		roles []user.Role
		want  error
		kept  []user.Role
	}{
		{"no roles", admin, nil, ErrInvalidRoles, nil},
		{"admin is never on a key", admin, []user.Role{user.RoleAdmin}, ErrInvalidRoles, nil},
		{"runtime is never on a key", admin, []user.Role{user.RoleRuntime}, ErrInvalidRoles, nil},
		{"unknown role", admin, []user.Role{"platform:root"}, ErrInvalidRoles, nil},
		{"a developer cannot mint operator", developer("d"), []user.Role{user.RoleOperator}, ErrEscalation, nil},
		{"roles are not a ladder: a developer cannot mint monitor", developer("d"), []user.Role{user.RoleMonitor}, ErrEscalation, nil},
		{"a developer may mint developer", developer("d"), []user.Role{user.RoleDeveloper}, nil, []user.Role{user.RoleDeveloper}},
		{
			"an admin may mint any key role, deduplicated in catalogue order",
			admin, []user.Role{user.RoleOperator, user.RoleMonitor, user.RoleOperator}, nil,
			[]user.Role{user.RoleMonitor, user.RoleOperator},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, _, err := NewService(newFakeRepo()).Create(
				context.Background(), tt.owner, "ci", tt.roles, time.Now().Add(time.Hour))
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if !slices.Equal(k.Roles, tt.kept) {
				t.Errorf("roles = %v, want %v", k.Roles, tt.kept)
			}
		})
	}
}

func TestVerifyHappyPathRecordsUse(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	k, token := create(t, svc, "user-1", "ci")

	got, err := svc.Verify(context.Background(), token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.UserID != "user-1" || got.ID != k.ID {
		t.Errorf("verify returned %+v, want owner user-1 / id %s", got, k.ID)
	}
	if repo.byID[k.ID].LastUsedAt == nil {
		t.Error("verify should record last-used (best-effort)")
	}
}

func TestVerifyRejectsUnknownExpiredAndRevoked(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)

	if _, err := svc.Verify(context.Background(), "octo_nope"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("unknown token: err = %v, want ErrInvalidToken", err)
	}

	// Expired: insert a key whose expiry is already past.
	_, expiredTok := create(t, svc, "user-1", "old")
	for id, k := range repo.byID {
		k.ExpiresAt = time.Now().Add(-time.Minute)
		repo.byID[id] = k
	}
	if _, err := svc.Verify(context.Background(), expiredTok); !errors.Is(err, ErrExpired) {
		t.Errorf("expired token: err = %v, want ErrExpired", err)
	}

	// Revoked: a deleted key verifies as invalid.
	k, revokedTok := create(t, svc, "user-2", "doomed")
	if err := svc.Delete(context.Background(), k.ID, "user-2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Verify(context.Background(), revokedTok); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("revoked token: err = %v, want ErrInvalidToken", err)
	}
}

func TestDeleteIsOwnerScoped(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	k, _ := create(t, svc, "owner", "k")

	// A different user cannot revoke it.
	if err := svc.Delete(context.Background(), k.ID, "intruder"); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-owner delete: err = %v, want ErrNotFound", err)
	}
	// The owner can.
	if err := svc.Delete(context.Background(), k.ID, "owner"); err != nil {
		t.Errorf("owner delete: %v", err)
	}
}

func TestListReturnsOnlyOwnersActiveKeys(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	a, _ := create(t, svc, "owner", "a")
	create(t, svc, "other", "b")

	items, err := svc.List(context.Background(), "owner")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != a.ID {
		t.Fatalf("list = %+v, want one key for owner", items)
	}
}
