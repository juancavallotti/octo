package kv

import (
	"bytes"
	"testing"
)

// key32 is a fixed 32-byte (AES-256) key for tests.
var key32 = bytes.Repeat([]byte{0x42}, 32)

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher(key32)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	plaintext := []byte("super secret token")
	sealed, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(sealed, plaintext) {
		t.Fatal("ciphertext contains the plaintext")
	}
	got, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round trip = %q, want %q", got, plaintext)
	}
}

func TestCipherNonceVariesPerEncrypt(t *testing.T) {
	c, _ := NewCipher(key32)
	a, _ := c.Encrypt([]byte("x"))
	b, _ := c.Encrypt([]byte("x"))
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions produced identical ciphertext (nonce reuse)")
	}
}

func TestCipherDecryptWithWrongKeyFails(t *testing.T) {
	enc, _ := NewCipher(key32)
	sealed, _ := enc.Encrypt([]byte("secret"))

	other, _ := NewCipher(bytes.Repeat([]byte{0x99}, 32))
	if _, err := other.Decrypt(sealed); err == nil {
		t.Fatal("decrypt with the wrong key should fail")
	}
}

func TestCipherRejectsBadKeyLength(t *testing.T) {
	if _, err := NewCipher([]byte("too short")); err == nil {
		t.Fatal("expected an error for an invalid key length")
	}
}

func TestCipherDecryptRejectsShortInput(t *testing.T) {
	c, _ := NewCipher(key32)
	if _, err := c.Decrypt([]byte("tiny")); err == nil {
		t.Fatal("expected an error for ciphertext shorter than the nonce")
	}
}
