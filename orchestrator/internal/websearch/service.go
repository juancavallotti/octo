// Package websearch holds the site-wide web search credential: the Parallel API
// key the platform agent searches the open web with. It stores and reveals; it
// does not call Parallel.
//
// It is optional in a way the LLM settings are not. Without a key the agent still
// installs and still answers — he simply holds a web_search tool that reports
// itself unavailable, which is why nothing here reports a blocked install.
package websearch

import (
	"context"
	"time"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

// repository is the persistence surface the service needs. Declared in the consumer
// (and unexported) so service tests can substitute a fake; *Repo satisfies it.
type repository interface {
	Get(ctx context.Context) (stored, error)
	Mutate(ctx context.Context, fn func(current stored) (stored, error)) error
}

// Service owns the web search settings.
type Service struct {
	repo   repository
	cipher *cryptox.Cipher
}

// NewService returns a Service. cipher may be nil, in which case the settings still
// read and a stored key can still be removed, but storing a new one is refused
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

// Update saves the settings. As everywhere else, a save that says nothing about
// the key keeps the stored one; an empty string removes it.
func (s *Service) Update(ctx context.Context, u Update) (Settings, error) {
	if err := validateUpdate(u); err != nil {
		return Settings{}, err
	}
	var saved Settings
	err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		key, err := cur.APIKey.Apply(u.APIKey, s.cipher)
		if err != nil {
			return stored{}, err
		}
		next := stored{APIKey: key, UpdatedAt: time.Now().UTC()}
		saved = next.toSettings()
		return next, nil
	})
	if err != nil {
		return Settings{}, err
	}
	return saved, nil
}

// Reveal returns the decrypted key for a server-side consumer — the agent
// installer, which writes it to a cluster secret. No handler calls it, and no
// route exposes it.
//
// An unconfigured installation is the expected case and not an error: it returns
// the empty string, and the caller binds the sentinel that tells the agent his
// web_search tool has no key behind it.
func (s *Service) Reveal(ctx context.Context) (string, error) {
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return "", err
	}
	if !cur.APIKey.Configured() {
		return "", nil
	}
	return cur.APIKey.Reveal(s.cipher)
}
