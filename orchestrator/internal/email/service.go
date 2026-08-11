// Package email holds the site's outbound email configuration — the provider key
// and the identity messages go out under — and the two ways that configuration is
// used: an operator-facing test send, and the send platform-internal callers make.
package email

import (
	"context"
	"strings"
	"time"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
	"github.com/juancavallotti/octo/orchestrator/internal/mailer"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// repository is the persistence surface the service needs. Declared in the consumer
// (and unexported) so service tests can substitute a fake; *Repo satisfies it.
type repository interface {
	Get(ctx context.Context) (stored, error)
	Mutate(ctx context.Context, fn func(current stored) (stored, error)) error
}

// sender is the outbound surface. *mailer.Resend satisfies it.
type sender interface {
	Send(ctx context.Context, m mailer.Message) (string, error)
}

// Service owns the email settings and the sends that use them.
type Service struct {
	repo   repository
	sender sender
	cipher *cryptox.Cipher
}

// NewService returns a Service. cipher may be nil, in which case the settings still
// read and their non-secret fields still save, but storing a new API key is refused
// rather than performed in the clear.
func NewService(repo repository, sender sender, cipher *cryptox.Cipher) *Service {
	return &Service{repo: repo, sender: sender, cipher: cipher}
}

// EncryptionAvailable reports whether an API key can be stored. The settings API
// surfaces this so an operator learns the answer before a save fails on it.
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
// stored one: every field on the form is submitted on every save, so the alternative
// is editing a from-address and destroying the key beside it.
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
		next := stored{
			Provider:  providerResend,
			FromEmail: strings.TrimSpace(u.FromEmail),
			FromName:  strings.TrimSpace(u.FromName),
			ReplyTo:   strings.TrimSpace(u.ReplyTo),
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

// SendTest sends a message using the draft on screen rather than what is stored, so
// what an operator verifies is what they are looking at. A key supplied with the
// request wins over the stored one, which is what makes testing a key *before*
// committing it possible; falling back to the stored one is what keeps the button
// working after a reload, when the form has no key in it.
func (s *Service) SendTest(ctx context.Context, t TestSend) (string, error) {
	if !validAddress(t.To) {
		return "", ErrInvalidRecipient
	}
	if err := validateUpdate(Update{
		APIKey:    t.APIKey,
		FromEmail: t.FromEmail,
		FromName:  t.FromName,
		ReplyTo:   t.ReplyTo,
	}); err != nil {
		return "", err
	}

	cur, err := s.repo.Get(ctx)
	if err != nil {
		return "", err
	}
	key, err := s.resolveKey(cur, t.APIKey)
	if err != nil {
		return "", err
	}

	draft := stored{
		FromEmail: strings.TrimSpace(t.FromEmail),
		FromName:  strings.TrimSpace(t.FromName),
		ReplyTo:   strings.TrimSpace(t.ReplyTo),
	}
	return s.sender.Send(ctx, mailer.Message{
		APIKey:  key,
		From:    draft.from(),
		ReplyTo: draft.ReplyTo,
		To:      []string{strings.TrimSpace(t.To)},
		Subject: "Test message from octo",
		Text: "This is a test message confirming that octo can send email " +
			"through the configured provider.",
	})
}

// Send delivers a message on behalf of the platform. Unlike SendTest it takes no key
// and no from-address: the identity a send goes out under is defined once, in the
// stored settings. That is what keeps this from being a relay for whatever key a
// caller happens to hold.
func (s *Service) Send(ctx context.Context, r SendRequest) (string, error) {
	if err := validateSendRequest(r); err != nil {
		return "", err
	}
	r = r.normalized()
	cur, err := s.repo.Get(ctx)
	if err != nil {
		return "", err
	}
	if cur.FromEmail == "" {
		return "", sitesettings.ErrNotConfigured
	}
	key, err := s.resolveKey(cur, nil)
	if err != nil {
		return "", err
	}
	return s.sender.Send(ctx, mailer.Message{
		APIKey:  key,
		From:    cur.from(),
		ReplyTo: cur.ReplyTo,
		To:      r.To,
		Subject: r.Subject,
		Text:    r.Text,
		HTML:    r.HTML,
	})
}

// resolveKey returns the key a send should use: the one supplied with the request if
// there is one, else the stored one decrypted.
func (s *Service) resolveKey(cur stored, supplied *string) (string, error) {
	if supplied != nil {
		if key := strings.TrimSpace(*supplied); key != "" {
			return key, nil
		}
	}
	return cur.APIKey.Reveal(s.cipher)
}
