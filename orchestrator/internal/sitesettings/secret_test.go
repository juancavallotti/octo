package sitesettings

import (
	"bytes"
	"errors"
	"testing"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

func testCipher(t *testing.T) *cryptox.Cipher {
	t.Helper()
	c, err := cryptox.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

// stored builds a SecretField holding key, the way a previous save would have.
func stored(t *testing.T, c *cryptox.Cipher, key string) SecretField {
	t.Helper()
	f, err := SecretField{}.Apply(&key, c)
	if err != nil {
		t.Fatalf("seed Apply: %v", err)
	}
	return f
}

func ptr(s string) *string { return &s }

// The headline invariant: a save that says nothing about the key must not touch it.
// Every settings form in the platform submits the other fields on every save, so if
// this regresses, editing a from-address silently destroys the API key beside it.
func TestApplyNilSuppliedPreservesStoredKey(t *testing.T) {
	c := testCipher(t)
	before := stored(t, c, "re_abcdefgh1234")

	after, err := before.Apply(nil, c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !bytes.Equal(after.Ciphertext, before.Ciphertext) {
		t.Fatal("ciphertext changed when the key was not supplied")
	}
	if after.Last4 != before.Last4 {
		t.Fatalf("last4 = %q, want %q", after.Last4, before.Last4)
	}
}

func TestApplyEmptySuppliedClearsKey(t *testing.T) {
	c := testCipher(t)
	before := stored(t, c, "re_abcdefgh1234")

	after, err := before.Apply(ptr(""), c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if after.Configured() {
		t.Fatal("key still configured after being cleared")
	}
	if after.Last4 != "" {
		t.Fatalf("last4 = %q, want empty", after.Last4)
	}
}

func TestApplyNewKeyReplacesAndRoundTrips(t *testing.T) {
	c := testCipher(t)
	before := stored(t, c, "re_oldkey12345")

	after, err := before.Apply(ptr("re_newkey56789"), c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if bytes.Equal(after.Ciphertext, before.Ciphertext) {
		t.Fatal("ciphertext unchanged after supplying a new key")
	}
	got, err := after.Reveal(c)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if got != "re_newkey56789" {
		t.Fatalf("Reveal = %q, want the new key", got)
	}
	if after.Last4 != "6789" {
		t.Fatalf("last4 = %q, want %q", after.Last4, "6789")
	}
}

// A supplied key is trimmed before it is sealed, so a value pasted with a trailing
// newline does not become a key that fails against the provider for no visible reason.
func TestApplyTrimsSuppliedKey(t *testing.T) {
	c := testCipher(t)

	after, err := SecretField{}.Apply(ptr("  re_padded1234\n"), c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := after.Reveal(c)
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if got != "re_padded1234" {
		t.Fatalf("Reveal = %q, want the trimmed key", got)
	}
}

// Whitespace-only is a cleared key, not a key made of spaces.
func TestApplyWhitespaceSuppliedClearsKey(t *testing.T) {
	c := testCipher(t)
	before := stored(t, c, "re_abcdefgh1234")

	after, err := before.Apply(ptr("   "), c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if after.Configured() {
		t.Fatal("whitespace should clear the key, not store one")
	}
}

func TestApplyWithoutCipherRejectsNewKey(t *testing.T) {
	before := SecretField{}

	after, err := before.Apply(ptr("re_abcdefgh1234"), nil)
	if !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatalf("err = %v, want ErrEncryptionUnavailable", err)
	}
	if after.Configured() {
		t.Fatal("a refused save must not return a stored key")
	}
}

// The other half of that rule: with no cipher you can still save the settings that
// sit beside the key, because carrying it forward never decrypts anything.
func TestApplyWithoutCipherPreservesAndClears(t *testing.T) {
	c := testCipher(t)
	before := stored(t, c, "re_abcdefgh1234")

	kept, err := before.Apply(nil, nil)
	if err != nil {
		t.Fatalf("Apply(nil, nil): %v", err)
	}
	if !bytes.Equal(kept.Ciphertext, before.Ciphertext) {
		t.Fatal("carrying the key forward should not need a cipher")
	}

	cleared, err := before.Apply(ptr(""), nil)
	if err != nil {
		t.Fatalf("Apply(\"\", nil): %v", err)
	}
	if cleared.Configured() {
		t.Fatal("clearing the key should not need a cipher")
	}
}

func TestRevealErrors(t *testing.T) {
	c := testCipher(t)

	if _, err := (SecretField{}).Reveal(c); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("empty field: err = %v, want ErrNotConfigured", err)
	}

	f := stored(t, c, "re_abcdefgh1234")
	if _, err := f.Reveal(nil); !errors.Is(err, ErrEncryptionUnavailable) {
		t.Fatalf("no cipher: err = %v, want ErrEncryptionUnavailable", err)
	}

	other, err := cryptox.NewCipher(bytes.Repeat([]byte{0x99}, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	if _, err := f.Reveal(other); err == nil {
		t.Fatal("decrypting with the wrong key should fail")
	}
}

// Nothing readable should survive a marshal: the ciphertext is base64 of AES-GCM
// output, and last4 is the only cleartext by design.
func TestSecretFieldMarshalHidesKey(t *testing.T) {
	c := testCipher(t)
	f := stored(t, c, "re_supersecret9f2a")

	if bytes.Contains(f.Ciphertext, []byte("re_supersecret")) {
		t.Fatal("ciphertext contains the plaintext key")
	}
	if f.Last4 != "9f2a" {
		t.Fatalf("last4 = %q, want %q", f.Last4, "9f2a")
	}
}
