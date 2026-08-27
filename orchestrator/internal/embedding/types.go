// Package embedding holds the site-wide embedding configuration: which provider
// and model the platform vectorizes agent memory with, and the API key it
// authenticates with. It stores, reveals, and — unlike the llm package — calls
// the provider, because the thing that needs a vector is a background sweep the
// orchestrator owns.
//
// Why the orchestrator and not the runtime: a runtime-side embedder would need a
// route that hands an API key to every pod, and a standalone-side one would make
// services import a connector, inverting the runtime's two extension points. So
// the vectors are produced where the credential already lives.
package embedding

import (
	"slices"
	"strings"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

const (
	// settingsKey is this feature's row in site_settings.
	settingsKey = "embedding"

	// The providers with an embeddings endpoint.
	//
	// Anthropic is absent and always will be: it has no embeddings API. That is
	// stated here rather than left as an omission because "why is Anthropic not in
	// the list" is the first question anyone asks, given it is in the llm one.
	ProviderOpenAI     = "OPENAI"
	ProviderGoogle     = "GOOGLE"
	ProviderOpenRouter = "OPENROUTER"

	// maxModelLen bounds the model identifier.
	maxModelLen = 200
	// minAPIKeyLen and maxAPIKeyLen bound a stored key, as in the llm package: short
	// enough to accept any real key, long enough that the stored last4 is not most
	// of the secret, and capped so a mistyped paste is refused before it is
	// encrypted.
	minAPIKeyLen = 8
	maxAPIKeyLen = 512
)

// Dimensions is the vector width every stored embedding has.
//
// It is a constant and not a setting because agent_turns.embedding is
// vector(1536): an indexable column needs a fixed width, and an HNSW index over a
// column whose width varies is not a thing. So the configured model must emit
// 1536 — natively for OpenAI's text-embedding-3-small, and through the provider's
// output-dimension parameter for the larger ones and for Gemini.
const Dimensions = 1536

// providers is the closed set a stored provider must belong to.
var providers = []string{ProviderOpenAI, ProviderGoogle, ProviderOpenRouter}

// Providers returns the closed set of providers a save may name.
func Providers() []string {
	return slices.Clone(providers)
}

// stored is the jsonb shape in site_settings.value.
type stored struct {
	Provider  string                   `json:"provider"`
	Model     string                   `json:"model"`
	APIKey    sitesettings.SecretField `json:"apiKey,omitzero"`
	UpdatedAt time.Time                `json:"updatedAt"`
}

// Settings is the read model. It carries no key material by construction.
type Settings struct {
	Provider   string     `json:"provider"`
	Model      string     `json:"model"`
	Dimensions int        `json:"dimensions"`
	Configured bool       `json:"configured"`
	Last4      string     `json:"last4,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

// Update is one save. APIKey is a pointer because "not supplied" and "supplied
// empty" are different instructions.
type Update struct {
	APIKey   *string `json:"apiKey"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
}

// Credentials is what the embedder needs to call the provider.
type Credentials struct {
	Provider string
	Model    string
	APIKey   string
}

// Configured reports whether these credentials can actually be used.
func (c Credentials) Configured() bool {
	return c.Provider != "" && c.Model != "" && c.APIKey != ""
}

// Status is what the admin page shows about the backfill: how much of the store
// has been vectorized, and how much has not.
//
// It exists because "embeddings are configured" and "search is semantic" are not
// the same statement — everything written before the provider was configured has
// no vector until the sweep reaches it, and an operator who has just turned this
// on deserves to see that happening rather than wonder why search has not
// changed.
type Status struct {
	Settings Settings `json:"settings"`
	Embedded int      `json:"embedded"`
	Pending  int      `json:"pending"`
	// EncryptionAvailable says whether a key can be stored at all. Without an
	// encryption key the provider and model still save and the form still works —
	// it is storing a credential that is refused, rather than performed in the
	// clear — so the page has to be able to say so before someone types one.
	EncryptionAvailable bool `json:"encryptionAvailable"`
}

// toSettings maps the stored row to the read model.
func (s stored) toSettings() Settings {
	out := Settings{
		Provider:   s.Provider,
		Model:      s.Model,
		Dimensions: Dimensions,
		Configured: s.APIKey.Configured(),
		Last4:      s.APIKey.Last4,
	}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

// validProvider reports whether p has an embeddings endpoint this can call.
func validProvider(p string) bool {
	return slices.Contains(providers, p)
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
