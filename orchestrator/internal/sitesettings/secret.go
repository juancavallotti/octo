package sitesettings

import (
	"strings"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

// last4Len is how much of a key is kept in the clear, so a UI can show which key
// is stored. Matches the api_keys table's last4 column, for the same reason.
const last4Len = 4

// SecretField is a provider API key held encrypted at rest, alongside the last few
// characters kept in the clear. The clear part is what lets a settings page say
// "the key ending 9f2a is stored" without any path existing that could read the
// key back out to a browser.
type SecretField struct {
	Ciphertext []byte `json:"ciphertext,omitempty"`
	Last4      string `json:"last4,omitempty"`
}

// Configured reports whether a key is stored.
func (f SecretField) Configured() bool {
	return len(f.Ciphertext) > 0
}

// Apply returns the field a save should persist. supplied is three-state, and each
// state is a different instruction from the caller:
//
//	nil  — the request said nothing about the key: keep the stored one
//	""   — the request cleared the key: remove it
//	else — replace it
//
// Keeping the stored key is the zero-work path rather than a branch someone has to
// remember to write, which is what makes "saving the settings form wiped my API
// key" hard to introduce by accident.
//
// A non-empty key with no cipher is refused rather than stored in the clear: see
// ErrEncryptionUnavailable. Note that this only affects a *changed* key — carrying
// the existing one forward copies ciphertext bytes and needs no cipher at all, so
// an install without an encryption key can still edit its other settings.
func (f SecretField) Apply(supplied *string, c *cryptox.Cipher) (SecretField, error) {
	if supplied == nil {
		return f, nil
	}
	key := strings.TrimSpace(*supplied)
	if key == "" {
		return SecretField{}, nil
	}
	if c == nil {
		return SecretField{}, ErrEncryptionUnavailable
	}
	ciphertext, err := c.Encrypt([]byte(key))
	if err != nil {
		return SecretField{}, err
	}
	return SecretField{Ciphertext: ciphertext, Last4: last4(key)}, nil
}

// Reveal decrypts the key for a server-side consumer — the code that actually calls
// the provider. Nothing on the HTTP surface calls this; a settings response carries
// Configured and Last4 and nothing more.
func (f SecretField) Reveal(c *cryptox.Cipher) (string, error) {
	if !f.Configured() {
		return "", ErrNotConfigured
	}
	if c == nil {
		return "", ErrEncryptionUnavailable
	}
	plaintext, err := c.Decrypt(f.Ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// last4 returns the trailing characters of key. It counts runes rather than bytes so
// a non-ASCII key cannot be split mid-character; callers reject keys shorter than
// last4Len, so the short branch is defensive rather than reachable.
func last4(key string) string {
	r := []rune(key)
	if len(r) <= last4Len {
		return string(r)
	}
	return string(r[len(r)-last4Len:])
}
