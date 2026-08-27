package embedding

import (
	"context"
	"strings"
	"time"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

// repository is the persistence surface the service needs. Declared in the
// consumer so service tests can substitute a fake; *Repo satisfies it.
type repository interface {
	Get(ctx context.Context) (stored, error)
	Mutate(ctx context.Context, fn func(current stored) (stored, error)) error
}

// counter reports how much of the store has been vectorized. The agent-memory
// repo satisfies it; it is an interface so this package does not depend on that
// one for a pair of integers.
type counter interface {
	EmbeddingCounts(ctx context.Context) (embedded, pending int, err error)
}

// Service owns the embedding configuration.
type Service struct {
	repo    repository
	cipher  *cryptox.Cipher
	counter counter
}

// NewService returns a Service. cipher may be nil, in which case the settings
// still read and the provider and model still save, but storing an API key is
// refused rather than performed in the clear. counter may be nil, in which case
// the status reports no counts.
func NewService(repo repository, cipher *cryptox.Cipher, c counter) *Service {
	return &Service{repo: repo, cipher: cipher, counter: c}
}

// EncryptionAvailable reports whether an API key can be stored.
func (s *Service) EncryptionAvailable() bool { return s.cipher != nil }

// Get returns the current settings, never the key.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return Settings{}, err
	}
	return cur.toSettings(), nil
}

// Status returns the settings alongside how far the backfill has got.
func (s *Service) Status(ctx context.Context) (Status, error) {
	settings, err := s.Get(ctx)
	if err != nil {
		return Status{}, err
	}
	out := Status{Settings: settings, EncryptionAvailable: s.EncryptionAvailable()}
	if s.counter != nil {
		embedded, pending, countErr := s.counter.EmbeddingCounts(ctx)
		if countErr != nil {
			return Status{}, countErr
		}
		out.Embedded, out.Pending = embedded, pending
	}
	return out, nil
}

// Update saves the settings, carrying the stored key forward when a save says
// nothing about it — except when the provider changes, where a key that
// authenticates against the old one is cleared rather than left reporting itself
// as configured. That is the llm package's reasoning and it holds identically
// here.
//
// What does NOT happen here, deliberately: changing the model does not re-embed
// anything, and does not clear what is stored. Vectors carry no record of which
// model produced them, so a table holding two models' vectors is not searchable
// either way, and ranking only the matching subset would silently halve the
// results instead of failing. Changing a model with rows already embedded is a
// user error the platform does not migrate for — the admin page says so at the
// point of change, which is the only place saying it helps.
func (s *Service) Update(ctx context.Context, u Update) (Settings, error) {
	if err := validateUpdate(u); err != nil {
		return Settings{}, err
	}
	var saved Settings
	err := s.repo.Mutate(ctx, func(cur stored) (stored, error) {
		supplied := u.APIKey
		if supplied == nil && cur.Provider != "" && cur.Provider != u.Provider {
			cleared := ""
			supplied = &cleared
		}
		key, err := cur.APIKey.Apply(supplied, s.cipher)
		if err != nil {
			return stored{}, err
		}
		next := stored{
			Provider:  u.Provider,
			Model:     strings.TrimSpace(u.Model),
			APIKey:    key,
			UpdatedAt: time.Now().UTC(),
		}
		saved = next.toSettings()
		return next, nil
	})
	if err != nil {
		return Settings{}, err
	}
	return saved, nil
}

// Clear removes the configuration entirely, which turns semantic search off.
//
// Stored vectors are left alone. They cost nothing where they are, they are still
// correct for the model that made them, and turning the same model back on should
// not mean re-embedding a whole history — which is the one case where a sweep is
// both expensive and completely unnecessary.
func (s *Service) Clear(ctx context.Context) error {
	return s.repo.Mutate(ctx, func(stored) (stored, error) {
		return stored{UpdatedAt: time.Now().UTC()}, nil
	})
}

// Reveal returns what the embedder needs to call the provider, including the
// decrypted key. No handler calls it and no route exposes it.
//
// A deployment that has never configured embeddings gets zero credentials and no
// error. That distinction is load-bearing: this is called on every sweep tick and
// every search, so "nobody has turned this on" reporting as a failure would put a
// warning in the log of every install that is working exactly as intended. An
// error here means something is actually wrong — a key that will not decrypt.
func (s *Service) Reveal(ctx context.Context) (Credentials, error) {
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return Credentials{}, err
	}
	if !cur.APIKey.Configured() {
		return Credentials{}, nil
	}
	key, err := cur.APIKey.Reveal(s.cipher)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Provider: cur.Provider, Model: cur.Model, APIKey: key}, nil
}
