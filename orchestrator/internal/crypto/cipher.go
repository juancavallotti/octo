// Package cryptox holds the orchestrator's symmetric encryption primitive: the
// AES-GCM cipher that protects values stored at rest in Postgres. It is shared by
// every feature that keeps a secret in a column — the KV store's secret namespaces
// and the site-wide provider API keys — so there is one construction, one stored
// format, and one place to change if the format ever has to move.
//
// The package is named cryptox rather than crypto so it does not shadow the
// standard library package of that name, which this file itself imports. That
// mirrors internal/http, which is package httpx for the same reason.
package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

// Cipher encrypts and decrypts values with AES-GCM. The stored form is
// nonce || ciphertext, so each value carries the nonce it was sealed with.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a raw key. The key length selects AES-128/192/256
// (16/24/32 bytes); other lengths are rejected.
func NewCipher(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher: gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext, returning nonce || ciphertext.
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("cipher: nonce: %w", err)
	}
	// Seal appends the ciphertext to nonce, so the result is nonce || ciphertext.
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt opens data shaped as nonce || ciphertext.
func (c *Cipher) Decrypt(data []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(data) < ns {
		return nil, errors.New("cipher: ciphertext too short")
	}
	nonce, ciphertext := data[:ns], data[ns:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("cipher: decrypt: %w", err)
	}
	return plaintext, nil
}
