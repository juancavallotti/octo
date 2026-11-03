package apikey

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/juancavallotti/octo/iam/internal/user"
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

// Create mints a key for owner carrying roles, which expires at expiresAt. It
// returns the stored record and the plaintext token; the plaintext is returned
// here and nowhere else, since only its hash is persisted.
//
// owner must carry the roles they hold now: a key may carry only roles that owner
// reaches, so the check is only as current as the user it is given.
func (s *Service) Create(
	ctx context.Context, owner user.User, name string, roles []user.Role, expiresAt time.Time,
) (APIKey, string, error) {
	if !validName(name) {
		return APIKey{}, "", ErrInvalidName
	}
	now := time.Now()
	if !expiresAt.After(now) || expiresAt.Sub(now) > maxTTL {
		return APIKey{}, "", ErrInvalidExpiry
	}
	granted, err := keyRoles(owner, roles)
	if err != nil {
		return APIKey{}, "", err
	}
	plaintext, hash, prefix, last4, err := generateToken()
	if err != nil {
		return APIKey{}, "", err
	}
	k := APIKey{
		UserID:    owner.ID,
		Name:      strings.TrimSpace(name),
		Prefix:    prefix,
		Last4:     last4,
		Roles:     granted,
		ExpiresAt: expiresAt,
	}
	stored, err := s.repo.Insert(ctx, k, hash)
	if err != nil {
		return APIKey{}, "", err
	}
	return stored, plaintext, nil
}

// keyRoles validates what a key is asked to carry and returns it deduplicated in
// catalogue order. An empty set, or a role no key may carry, is ErrInvalidRoles;
// a role owner does not reach is ErrEscalation, naming it.
func keyRoles(owner user.User, roles []user.Role) ([]user.Role, error) {
	if len(roles) == 0 {
		return nil, fmt.Errorf("%w: a key needs at least one role", ErrInvalidRoles)
	}
	for _, r := range roles {
		if !user.ValidKeyRole(r) {
			return nil, fmt.Errorf("%w: %q cannot be carried by a key", ErrInvalidRoles, string(r))
		}
		if !owner.Reaches(r) {
			return nil, fmt.Errorf("%w: %s", ErrEscalation, r)
		}
	}
	var ret []user.Role
	for _, r := range user.KeyRoles() {
		if slices.Contains(roles, r) {
			ret = append(ret, r)
		}
	}
	return ret, nil
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
