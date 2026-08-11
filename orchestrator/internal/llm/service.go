// Package llm holds the site-wide LLM provider configuration: which provider and
// model the platform's own agent reasons with, and the API key it authenticates
// with. It stores and reveals; it does not call the provider.
//
// This is deliberately separate from the per-connector keys an integration
// configures. Those belong to the integration that uses them; this one belongs to
// the installation, and nothing in the runtime reads it.
package llm

import (
	"context"
	"strings"
	"time"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

// repository is the persistence surface the service needs. Declared in the consumer
// (and unexported) so service tests can substitute a fake; *Repo satisfies it.
type repository interface {
	Get(ctx context.Context) (stored, error)
	Put(ctx context.Context, s stored) error
}

// Service owns the LLM provider settings.
type Service struct {
	repo   repository
	cipher *cryptox.Cipher
}

// NewService returns a Service. cipher may be nil, in which case the settings still
// read and the provider and model still save, but storing a new API key is refused
// rather than performed in the clear.
func NewService(repo repository, cipher *cryptox.Cipher) *Service {
	return &Service{repo: repo, cipher: cipher}
}

// EncryptionAvailable reports whether an API key can be stored.
func (s *Service) EncryptionAvailable() bool {
	return s.cipher != nil
}

// Get returns the current settings, never the key.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	return cur.toSettings(), nil
}

// Update saves the settings. A save that says nothing about the API key keeps the
// stored one, so switching model or provider does not destroy the credentials.
func (s *Service) Update(ctx context.Context, u Update) (Settings, error) {
	if err := validateUpdate(u); err != nil {
		return Settings{}, err
	}
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return Settings{}, err
	}

	key, err := cur.APIKey.Apply(u.APIKey, s.cipher)
	if err != nil {
		return Settings{}, err
	}

	next := stored{
		Provider:  u.Provider,
		Model:     strings.TrimSpace(u.Model),
		APIKey:    key,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.Put(ctx, next); err != nil {
		return Settings{}, err
	}
	return next.toSettings(), nil
}

// Reveal returns everything a server-side consumer needs to call the provider,
// including the decrypted key. It exists for the agent work that will read this
// configuration; no handler calls it, and no route exposes it.
func (s *Service) Reveal(ctx context.Context) (Credentials, error) {
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return Credentials{}, err
	}
	key, err := cur.APIKey.Reveal(s.cipher)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Provider: cur.Provider, Model: cur.Model, APIKey: key}, nil
}
