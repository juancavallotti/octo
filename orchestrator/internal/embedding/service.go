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

// vectors is the stored-vector surface this package needs: how much is embedded,
// and how to discard it. The agent-memory repo satisfies it; it is an interface so
// this package does not depend on that one.
type vectors interface {
	EmbeddingCounts(ctx context.Context) (embedded, pending int, err error)
	ClearEmbeddings(ctx context.Context) error
}

// Service owns the embedding configuration.
type Service struct {
	repo    repository
	cipher  *cryptox.Cipher
	vectors vectors
}

// NewService returns a Service. cipher may be nil, in which case the settings
// still read and the provider and model still save, but storing an API key is
// refused rather than performed in the clear. v may be nil, in which case the
// status reports no counts and nothing can be discarded.
func NewService(repo repository, cipher *cryptox.Cipher, v vectors) *Service {
	return &Service{repo: repo, cipher: cipher, vectors: v}
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
	if s.vectors != nil {
		embedded, pending, countErr := s.vectors.EmbeddingCounts(ctx)
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
// Changing the provider or the model DISCARDS every stored vector.
//
// The platform does not migrate embeddings and does not intend to. What it also
// must not do is leave two embedding spaces in one store: vectors carry no record
// of which model produced them, so a table holding both is not searchable either
// way, and ranking only the matching subset would silently halve the results
// rather than fail. Keeping the store to one space at a time is what makes the
// absence of provenance safe rather than a latent corruption.
//
// So the vectors go and the sweep rebuilds them from text that is still there.
// The rows are untouched, search keeps working on the text index throughout, and
// the only cost is the re-embedding — which is the cost of the change, and is
// exactly what the admin page warns about before making it.
func (s *Service) Update(ctx context.Context, u Update) (Settings, error) {
	if err := validateUpdate(u); err != nil {
		return Settings{}, err
	}
	var spaceChanged bool
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
		// "Had a space, and it is not this one." A first configuration changes
		// nothing, because there is nothing embedded to be inconsistent with.
		spaceChanged = (cur.Provider != "" && cur.Provider != next.Provider) ||
			(cur.Model != "" && cur.Model != next.Model)
		saved = next.toSettings()
		return next, nil
	})
	if err != nil {
		return Settings{}, err
	}
	if spaceChanged {
		// After the settings are written, not before: a discard that ran first and
		// then failed to save would have thrown the vectors away for a change that
		// did not happen.
		if err := s.discardVectors(ctx); err != nil {
			return Settings{}, err
		}
	}
	return saved, nil
}

// discardVectors throws away every stored vector, so the store holds one
// embedding space at a time. See Update.
func (s *Service) discardVectors(ctx context.Context) error {
	if s.vectors == nil {
		return nil
	}
	return s.vectors.ClearEmbeddings(ctx)
}

// Clear removes the configuration entirely, which turns semantic search off, and
// discards the stored vectors with it.
//
// Keeping them would be cheaper for the operator who turns the same model back
// on — and it is the case that makes the absence of provenance unsafe. Once the
// settings are gone, nothing records which model made those vectors, so the next
// configuration could be a different model and the store would silently hold two
// spaces. Discarding is what keeps "there is no per-row model" a simplification
// rather than a bug waiting for someone to toggle a setting twice.
func (s *Service) Clear(ctx context.Context) error {
	if err := s.repo.Mutate(ctx, func(stored) (stored, error) {
		return stored{UpdatedAt: time.Now().UTC()}, nil
	}); err != nil {
		return err
	}
	return s.discardVectors(ctx)
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
