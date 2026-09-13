package cryptox

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

// The plaintext every round-trip below is checked against. It is deliberately
// not a round number of blocks.
const samplePlaintext = "the launch codes are 0000"

// ciphers enumerates the symmetric algorithms with a key each one accepts, so
// the properties that must hold for all of them are only written once.
func ciphers(t *testing.T) map[string]Cipher {
	t.Helper()

	aesGCM, err := NewAESGCM(bytes.Repeat([]byte{'k'}, aes256KeyLen))
	if err != nil {
		t.Fatalf("build aes-gcm: %v", err)
	}
	chacha, err := NewChaCha20Poly1305(bytes.Repeat([]byte{'k'}, aes256KeyLen))
	if err != nil {
		t.Fatalf("build chacha20-poly1305: %v", err)
	}
	return map[string]Cipher{"aes-gcm": aesGCM, "chacha20-poly1305": chacha}
}

func TestRoundTrip(t *testing.T) {
	for name, cipher := range ciphers(t) {
		t.Run(name, func(t *testing.T) {
			sealed, err := cipher.Seal([]byte(samplePlaintext))
			if err != nil {
				t.Fatalf("seal: %v", err)
			}
			opened, err := cipher.Open(sealed)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if string(opened) != samplePlaintext {
				t.Errorf("got %q, want %q", opened, samplePlaintext)
			}
		})
	}
}

// A fresh nonce per call is what keeps two equal plaintexts from producing two
// equal ciphertexts, which would leak that they were equal.
func TestSealIsNotDeterministic(t *testing.T) {
	for name, cipher := range ciphers(t) {
		t.Run(name, func(t *testing.T) {
			first, err := cipher.Seal([]byte(samplePlaintext))
			if err != nil {
				t.Fatalf("seal: %v", err)
			}
			second, err := cipher.Seal([]byte(samplePlaintext))
			if err != nil {
				t.Fatalf("seal again: %v", err)
			}
			if bytes.Equal(first, second) {
				t.Error("sealing the same plaintext twice produced identical bytes")
			}
		})
	}
}

// This is the test that proves the authentication tag is actually checked. If a
// flipped bit opens, the algorithm has degenerated into plain encryption and
// nothing downstream can trust what it reads back.
func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	for name, cipher := range ciphers(t) {
		t.Run(name, func(t *testing.T) {
			sealed, err := cipher.Seal([]byte(samplePlaintext))
			if err != nil {
				t.Fatalf("seal: %v", err)
			}
			sealed[len(sealed)-1] ^= 0xff

			if _, err := cipher.Open(sealed); err == nil {
				t.Error("a tampered ciphertext opened")
			}
		})
	}
}

func TestOpenRejectsWrongKey(t *testing.T) {
	sealer, err := NewAESGCM(bytes.Repeat([]byte{'a'}, aes256KeyLen))
	if err != nil {
		t.Fatalf("build sealer: %v", err)
	}
	opener, err := NewAESGCM(bytes.Repeat([]byte{'b'}, aes256KeyLen))
	if err != nil {
		t.Fatalf("build opener: %v", err)
	}

	sealed, err := sealer.Seal([]byte(samplePlaintext))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := opener.Open(sealed); err == nil {
		t.Error("a value sealed under one key opened under another")
	}
}

func TestOpenRejectsValueShorterThanNonce(t *testing.T) {
	cipher, err := NewAESGCM(bytes.Repeat([]byte{'k'}, aes256KeyLen))
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	if _, err := cipher.Open([]byte{1, 2, 3}); err == nil {
		t.Error("a value too short to hold a nonce opened")
	}
}

// AES takes three key lengths and chacha takes exactly one; anything else has to
// be reported with the length it actually got, because a key arrives base64- or
// hex-decoded and the length is the only clue to what went wrong.
func TestKeyLengthValidation(t *testing.T) {
	cases := []struct {
		name  string
		build func([]byte) (Cipher, error)
		key   int
		ok    bool
	}{
		{"aes-128", NewAESGCM, aes128KeyLen, true},
		{"aes-192", NewAESGCM, aes192KeyLen, true},
		{"aes-256", NewAESGCM, aes256KeyLen, true},
		{"aes-short", NewAESGCM, 8, false},
		{"aes-odd", NewAESGCM, 20, false},
		{"chacha-256", NewChaCha20Poly1305, aes256KeyLen, true},
		{"chacha-128", NewChaCha20Poly1305, aes128KeyLen, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build(bytes.Repeat([]byte{'k'}, tc.key))
			if tc.ok {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("a key of the wrong length was accepted")
			}
			if !strings.Contains(err.Error(), "bytes") {
				t.Errorf("the error should say how long the key was, got %q", err)
			}
		})
	}
}

func TestLengthList(t *testing.T) {
	if got := lengthList([]int{32}); got != "32 bytes" {
		t.Errorf("got %q", got)
	}
	if got := lengthList([]int{16, 24, 32}); got != "16, 24 or 32 bytes" {
		t.Errorf("got %q", got)
	}
}

// rsaKeys generates a keypair and renders it the way an operator would supply
// it: PKIX for the public half, PKCS#8 for the private one.
func rsaKeys(t *testing.T) (publicPEM, privatePEM []byte) {
	t.Helper()

	// 2048 is the smallest modulus worth using, and generating it is the slowest
	// thing in this file, so tests share one pair per call rather than per case.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
}

func TestRSARoundTrip(t *testing.T) {
	publicPEM, privatePEM := rsaKeys(t)

	cipher, err := NewRSAOAEP(publicPEM, privatePEM)
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}

	sealed, err := cipher.Seal([]byte(samplePlaintext))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	opened, err := cipher.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != samplePlaintext {
		t.Errorf("got %q, want %q", opened, samplePlaintext)
	}
}

// The asymmetric case people actually want: a flow that can seal but not open,
// because only the public half was handed to it.
func TestRSAPublicKeyOnlyCannotOpen(t *testing.T) {
	publicPEM, _ := rsaKeys(t)

	cipher, err := NewRSAOAEP(publicPEM, nil)
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}
	if _, err := cipher.Seal([]byte(samplePlaintext)); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := cipher.Open([]byte("anything")); err == nil {
		t.Error("a public-key-only cipher opened a value")
	}
}

// A private key carries its public half, so supplying only the private key still
// leaves a cipher that can do both.
func TestRSAPrivateKeyAloneCanSeal(t *testing.T) {
	_, privatePEM := rsaKeys(t)

	cipher, err := NewRSAOAEP(nil, privatePEM)
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}

	sealed, err := cipher.Seal([]byte(samplePlaintext))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	opened, err := cipher.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != samplePlaintext {
		t.Errorf("got %q, want %q", opened, samplePlaintext)
	}
}

func TestRSARejectsPlaintextLargerThanTheModulus(t *testing.T) {
	publicPEM, privatePEM := rsaKeys(t)

	cipher, err := NewRSAOAEP(publicPEM, privatePEM)
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}

	_, err = cipher.Seal(bytes.Repeat([]byte{'x'}, 1024))
	if err == nil {
		t.Fatal("a plaintext larger than the modulus was encrypted")
	}
	if !strings.Contains(err.Error(), "at most") {
		t.Errorf("the error should say how much this key holds, got %q", err)
	}
}

func TestRSARejectsUnusableKeyMaterial(t *testing.T) {
	if _, err := NewRSAOAEP(nil, nil); err == nil {
		t.Error("a cipher with no keys at all was built")
	}
	if _, err := NewRSAOAEP([]byte("not pem"), nil); err == nil {
		t.Error("a public key that is not PEM was accepted")
	}
	if _, err := NewRSAOAEP(nil, []byte("not pem")); err == nil {
		t.Error("a private key that is not PEM was accepted")
	}
}
