package llm

import (
	"strings"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

const (
	// settingsKey is this feature's row in site_settings.
	settingsKey = "llm"

	// The providers the runtime can talk to. These mirror core.ProviderAnthropic and
	// friends in runtime/core/llm.go, which is the source of truth — but the runtime
	// is a separate Go module and the orchestrator does not depend on it. Taking on
	// that dependency to share three string constants would be the wrong trade; if a
	// fourth provider appears there, it is added here too.
	ProviderAnthropic = "ANTHROPIC"
	ProviderOpenAI    = "OPENAI"
	ProviderGoogle    = "GOOGLE"

	// maxModelLen bounds the model identifier.
	maxModelLen = 200
	// minAPIKeyLen is short enough to accept any real provider key and long enough
	// that the stored last4 is not most of the secret. maxAPIKeyLen is far above any
	// real key and exists so a mistyped paste — a whole file, say — is refused here
	// rather than encrypted and stored.
	minAPIKeyLen = 8
	maxAPIKeyLen = 512
)

// providers is the closed set a stored provider must belong to.
var providers = []string{ProviderAnthropic, ProviderOpenAI, ProviderGoogle}

// stored is the jsonb shape in site_settings.value. Unexported: nothing outside this
// package sees the ciphertext.
type stored struct {
	Provider  string                   `json:"provider"`
	Model     string                   `json:"model"`
	APIKey    sitesettings.SecretField `json:"apiKey,omitzero"`
	UpdatedAt time.Time                `json:"updatedAt"`
}

// Settings is the read model. It carries no key material by construction.
type Settings struct {
	Provider   string
	Model      string
	Configured bool
	Last4      string
	UpdatedAt  *time.Time
}

// Update is one save. APIKey is a pointer because "not supplied" and "supplied
// empty" are different instructions; see sitesettings.SecretField.Apply.
type Update struct {
	APIKey   *string
	Provider string
	Model    string
}

// Credentials is what a server-side consumer needs to actually call the provider.
// It is returned by Reveal and never by a handler.
type Credentials struct {
	Provider string
	Model    string
	APIKey   string
}

// toSettings maps the stored row to the read model.
func (s stored) toSettings() Settings {
	out := Settings{
		Provider:   s.Provider,
		Model:      s.Model,
		Configured: s.APIKey.Configured(),
		Last4:      s.APIKey.Last4,
	}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

// validProvider reports whether p is one of the providers the runtime supports.
func validProvider(p string) bool {
	for _, known := range providers {
		if p == known {
			return true
		}
	}
	return false
}

// validateUpdate checks a save before anything is written or encrypted.
func validateUpdate(u Update) error {
	if !validProvider(u.Provider) {
		return ErrInvalidProvider
	}
	model := strings.TrimSpace(u.Model)
	if model == "" || len(model) > maxModelLen || strings.ContainsAny(model, "\r\n") {
		return ErrInvalidModel
	}
	if u.APIKey != nil {
		if trimmed := strings.TrimSpace(*u.APIKey); trimmed != "" &&
			(len(trimmed) < minAPIKeyLen || len(trimmed) > maxAPIKeyLen) {
			return ErrInvalidAPIKey
		}
	}
	return nil
}
