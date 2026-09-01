package websearch

import (
	"strings"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

const (
	// settingsKey is this feature's row in site_settings.
	settingsKey = "websearch"

	// Provider is the only web search provider this platform talks to. It is a
	// constant rather than a stored field because there is nothing to choose
	// between: the agent's tool is a parallel-search block, so a second provider
	// would be a second connector and a second tool, not a different value here.
	// It is exported so a caller — the admin page, a future second provider's
	// migration — can name it without restating the string.
	Provider = "PARALLEL"

	// minAPIKeyLen and maxAPIKeyLen bound a key the same way the LLM settings do:
	// short enough to accept any real key, long enough that last4 is not most of
	// the secret, and capped so a mistyped paste is refused before it is encrypted.
	minAPIKeyLen = 8
	maxAPIKeyLen = 512
)

// stored is the jsonb shape in site_settings.value. Unexported: nothing outside
// this package sees the ciphertext.
type stored struct {
	APIKey    sitesettings.SecretField `json:"apiKey,omitzero"`
	UpdatedAt time.Time                `json:"updatedAt"`
}

// Settings is the read model. It carries no key material by construction.
type Settings struct {
	Provider   string
	Configured bool
	Last4      string
	UpdatedAt  *time.Time
}

// Update is one save. APIKey is a pointer because "not supplied" and "supplied
// empty" are different instructions; see sitesettings.SecretField.Apply.
//
// Unlike the LLM settings there is no other field, so a save that says nothing
// about the key is a no-op rather than a way to change something else. It is still
// accepted: refusing it would make the handler's three-state contract a lie.
type Update struct {
	APIKey *string
}

// toSettings maps the stored row to the read model.
func (s stored) toSettings() Settings {
	out := Settings{
		Provider:   Provider,
		Configured: s.APIKey.Configured(),
		Last4:      s.APIKey.Last4,
	}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

// validateUpdate checks a save before anything is written or encrypted.
func validateUpdate(u Update) error {
	if u.APIKey != nil {
		if trimmed := strings.TrimSpace(*u.APIKey); trimmed != "" &&
			(len(trimmed) < minAPIKeyLen || len(trimmed) > maxAPIKeyLen) {
			return ErrInvalidAPIKey
		}
	}
	return nil
}
