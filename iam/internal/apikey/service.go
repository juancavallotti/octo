package apikey

import (
	"context"
	"errors"
	"strings"
	"time"
)

// repository is the persistence surface the service needs. Declared in the
// consumer (and unexported) so service tests can substitute a fake; *Repo
// satisfies it structurally.
type repository interface {
	Insert(ctx context.Context, k APIKey, keyHash string) (APIKey, error)
	ListByUser(ctx context.Context, userID string) ([]APIKey, error)
	GetByHash(ctx context.Context, keyHash string) (APIKey, error)
	Revoke(ctx context.Context, id, userID string) error
	TouchLastUsed(ctx context.Context, id string) error
}

// Service holds API-key lifecycle logic: minting, listing, revoking, and verifying
// bearer tokens.
type Service struct {
	repo repository
}

// NewService returns a Service backed by repo.
func NewService(repo repository) *Service {
	return &Service{repo: repo}
}

// Create mints a key for userID that expires after ttl. It returns the stored
// record and the plaintext token; the plaintext is returned here and nowhere else,
// since only its hash is persisted.
func (s *Service) Create(ctx context.Context, userID, name string, ttl time.Duration) (APIKey, string, error) {
	if !validName(name) {
		return APIKey{}, "", ErrInvalidName
	}
	if ttl <= 0 || ttl > maxTTL {
		return APIKey{}, "", ErrInvalidTTL
	}
	plaintext, hash, prefix, last4, err := generateToken()
	if err != nil {
		return APIKey{}, "", err
	}
	k := APIKey{
		UserID:    userID,
		Name:      strings.TrimSpace(name),
		Prefix:    prefix,
		Last4:     last4,
		ExpiresAt: time.Now().Add(ttl),
	}
	stored, err := s.repo.Insert(ctx, k, hash)
	if err != nil {
		return APIKey{}, "", err
	}
	return stored, plaintext, nil
}

// List returns a user's active keys (no secret material).
func (s *Service) List(ctx context.Context, userID string) ([]APIKey, error) {
	return s.repo.ListByUser(ctx, userID)
}

// Delete revokes a key, scoped to its owner.
func (s *Service) Delete(ctx context.Context, id, userID string) error {
	return s.repo.Revoke(ctx, id, userID)
}

// Verify resolves a presented plaintext token to its active, unexpired key. A
// token matching no active key is ErrInvalidToken; a matched-but-past-expiry key
// is ErrExpired. On success it records the use (best-effort) and returns the key,
// whose UserID identifies the caller.
func (s *Service) Verify(ctx context.Context, plaintext string) (APIKey, error) {
	plaintext = strings.TrimSpace(plaintext)
	if plaintext == "" {
		return APIKey{}, ErrInvalidToken
	}
	k, err := s.repo.GetByHash(ctx, hashToken(plaintext))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return APIKey{}, ErrInvalidToken
		}
		return APIKey{}, err
	}
	if k.RevokedAt != nil {
		return APIKey{}, ErrInvalidToken
	}
	if time.Now().After(k.ExpiresAt) {
		return APIKey{}, ErrExpired
	}
	_ = s.repo.TouchLastUsed(ctx, k.ID) // best-effort; must not fail the request
	return k, nil
}
