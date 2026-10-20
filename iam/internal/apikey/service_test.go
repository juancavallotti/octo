package apikey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
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

func TestCreateReturnsRecognizableTokenAndStoresOnlyHash(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)

	k, token, err := svc.Create(context.Background(), "user-1", "ci", time.Hour)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
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

func TestCreateRejectsBadNameAndTTL(t *testing.T) {
	svc := NewService(newFakeRepo())
	if _, _, err := svc.Create(context.Background(), "u", "  ", time.Hour); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty name: err = %v, want ErrInvalidName", err)
	}
	if _, _, err := svc.Create(context.Background(), "u", "ok", 0); !errors.Is(err, ErrInvalidTTL) {
		t.Errorf("zero ttl: err = %v, want ErrInvalidTTL", err)
	}
	if _, _, err := svc.Create(context.Background(), "u", "ok", maxTTL+time.Hour); !errors.Is(err, ErrInvalidTTL) {
		t.Errorf("excessive ttl: err = %v, want ErrInvalidTTL", err)
	}
}

func TestVerifyHappyPathRecordsUse(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo)
	k, token, _ := svc.Create(context.Background(), "user-1", "ci", time.Hour)

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
	_, expiredTok, _ := svc.Create(context.Background(), "user-1", "old", time.Hour)
	for id, k := range repo.byID {
		k.ExpiresAt = time.Now().Add(-time.Minute)
		repo.byID[id] = k
	}
	if _, err := svc.Verify(context.Background(), expiredTok); !errors.Is(err, ErrExpired) {
		t.Errorf("expired token: err = %v, want ErrExpired", err)
	}

	// Revoked: a deleted key verifies as invalid.
	k, revokedTok, _ := svc.Create(context.Background(), "user-2", "doomed", time.Hour)
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
	k, _, _ := svc.Create(context.Background(), "owner", "k", time.Hour)

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
	a, _, _ := svc.Create(context.Background(), "owner", "a", time.Hour)
	_, _, _ = svc.Create(context.Background(), "other", "b", time.Hour)

	items, err := svc.List(context.Background(), "owner")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != a.ID {
		t.Fatalf("list = %+v, want one key for owner", items)
	}
}
