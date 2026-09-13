package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

// The key lengths each symmetric algorithm accepts. AES takes three, and which
// one is supplied is what selects AES-128, AES-192 or AES-256.
const (
	aes128KeyLen = 16
	aes192KeyLen = 24
	aes256KeyLen = 32
)

// aeadCipher seals with an AEAD construction. The sealed form is
// nonce || ciphertext, so each value carries the nonce it was sealed with and a
// caller never has to keep one alongside.
type aeadCipher struct {
	name string
	aead cipher.AEAD
}

// NewAESGCM builds an AES-GCM cipher. The key length selects the variant:
// 16 bytes for AES-128, 24 for AES-192, 32 for AES-256.
//
//nolint:ireturn // the constructors return the Cipher interface so a caller can hold any algorithm
func NewAESGCM(key []byte) (Cipher, error) {
	const name = "aes-gcm"

	if err := requireKeyLen(name, key, aes128KeyLen, aes192KeyLen, aes256KeyLen); err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &aeadCipher{name: name, aead: aead}, nil
}

// NewChaCha20Poly1305 builds a ChaCha20-Poly1305 cipher, which takes a 32-byte
// key and nothing else.
//
//nolint:ireturn // the constructors return the Cipher interface so a caller can hold any algorithm
func NewChaCha20Poly1305(key []byte) (Cipher, error) {
	const name = "chacha20-poly1305"

	if err := requireKeyLen(name, key, chacha20poly1305.KeySize); err != nil {
		return nil, err
	}

	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &aeadCipher{name: name, aead: aead}, nil
}

// Seal returns nonce || ciphertext. The nonce is fresh for every call, so
// sealing the same plaintext twice does not produce the same bytes — which is
// the point: equal ciphertexts would leak that the values are equal.
func (c *aeadCipher) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%s: read nonce: %w", c.name, err)
	}
	// Seal appends the ciphertext to the nonce we pass as the destination, so
	// what comes back is already nonce || ciphertext.
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open splits the nonce back off and authenticates the rest. A wrong key and a
// tampered ciphertext fail the same way, and the error says no more than that:
// which of the two it was is not something we can tell, nor something worth
// guessing at out loud.
func (c *aeadCipher) Open(sealed []byte) ([]byte, error) {
	size := c.aead.NonceSize()
	if len(sealed) < size {
		return nil, fmt.Errorf("%s: the value is too short to have been sealed by this cipher", c.name)
	}

	nonce, ciphertext := sealed[:size], sealed[size:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"%s: could not open the value; it was sealed with a different key, or it has been altered", c.name)
	}
	return plaintext, nil
}

// requireKeyLen checks the key against the lengths an algorithm accepts, so a
// mistyped key is reported with the length it actually had rather than through
// whatever the underlying package happens to say.
func requireKeyLen(name string, key []byte, accepted ...int) error {
	for _, want := range accepted {
		if len(key) == want {
			return nil
		}
	}
	return fmt.Errorf("%s: key is %d bytes, but this algorithm takes %s", name, len(key), lengthList(accepted))
}

// lengthList renders the accepted lengths as a phrase a person can read:
// "32 bytes", or "16, 24 or 32 bytes".
func lengthList(accepted []int) string {
	parts := make([]string, len(accepted))
	for i, n := range accepted {
		parts[i] = strconv.Itoa(n)
	}
	switch len(parts) {
	case 1:
		return parts[0] + " bytes"
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1] + " bytes"
	}
}
