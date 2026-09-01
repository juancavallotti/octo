package websearch

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// fakeRepo holds one row in memory and counts writes, so a test can assert that a
// rejected request wrote nothing.
type fakeRepo struct {
	row      stored
	putCalls int
}

func (f *fakeRepo) Get(context.Context) (stored, error) { return f.row, nil }

func (f *fakeRepo) Mutate(_ context.Context, fn func(stored) (stored, error)) error {
	next, err := fn(f.row)
	if err != nil {
		return err
	}
	f.putCalls++
	f.row = next
	return nil
}

func testCipher(t *testing.T) *cryptox.Cipher {
	t.Helper()
	c, err := cryptox.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func ptr(s string) *string { return &s }

// newService wires a service over a fake repo, seeded with key when non-empty.
func newService(t *testing.T, key string) (*Service, *fakeRepo) {
	t.Helper()
	c := testCipher(t)
	repo := &fakeRepo{}
	if key != "" {
		field, err := sitesettings.SecretField{}.Apply(&key, c)
		if err != nil {
			t.Fatalf("seed key: %v", err)
		}
		repo.row.APIKey = field
	}
	return NewService(repo, c), repo
}

func TestUpdateValidation(t *testing.T) {
	tests := []struct {
		name string
		key  *string
		want error
	}{
		{"short api key", ptr("abc"), ErrInvalidAPIKey},
		{"over-long api key", ptr(strings.Repeat("k", maxAPIKeyLen+1)), ErrInvalidAPIKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newService(t, "")

			if _, err := svc.Update(context.Background(), Update{APIKey: tc.key}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if repo.putCalls != 0 {
				t.Fatalf("putCalls = %d, want 0 — a rejected save must not write", repo.putCalls)
			}
		})
	}
}

func TestUpdateStoresTheKeyEncrypted(t *testing.T) {
	svc, repo := newService(t, "")

	const key = "parallel-key-9f2a"
	got, err := svc.Update(context.Background(), Update{APIKey: ptr(key)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !got.Configured || got.Last4 != "9f2a" {
		t.Fatalf("settings = %+v, want configured with last4 9f2a", got)
	}
	if got.Provider != Provider {
		t.Fatalf("provider = %q, want %q", got.Provider, Provider)
	}
	if bytes.Contains(repo.row.APIKey.Ciphertext, []byte(key)) {
		t.Fatal("the stored ciphertext contains the key in the clear")
	}
}

// The three states of a save, which is the whole contract of this endpoint.
func TestUpdateKeyStates(t *testing.T) {
	t.Run("omitted keeps the stored key", func(t *testing.T) {
		svc, repo := newService(t, "parallel-stored-key")
		before := repo.row.APIKey

		got, err := svc.Update(context.Background(), Update{})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if !got.Configured {
			t.Fatal("configured = false — an omitted key must not clear the stored one")
		}
		if !bytes.Equal(repo.row.APIKey.Ciphertext, before.Ciphertext) {
			t.Fatal("the stored ciphertext changed on a save that said nothing about the key")
		}
	})

	t.Run("empty removes it", func(t *testing.T) {
		svc, repo := newService(t, "parallel-stored-key")

		got, err := svc.Update(context.Background(), Update{APIKey: ptr("")})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.Configured || repo.row.APIKey.Configured() {
			t.Fatal("the key survived a save that cleared it")
		}
	})
}

// Carrying the existing key forward copies ciphertext and needs no cipher, so an
// install without an encryption key can still clear one — it just cannot store one.
func TestUpdateWithoutCipher(t *testing.T) {
	svc := NewService(&fakeRepo{}, nil)

	_, err := svc.Update(context.Background(), Update{APIKey: ptr("parallel-key-1234")})
	if !errors.Is(err, sitesettings.ErrEncryptionUnavailable) {
		t.Fatalf("err = %v, want ErrEncryptionUnavailable", err)
	}
	if svc.EncryptionAvailable() {
		t.Fatal("EncryptionAvailable = true with no cipher")
	}
}

func TestReveal(t *testing.T) {
	t.Run("returns the stored key", func(t *testing.T) {
		const key = "parallel-stored-key"
		svc, _ := newService(t, key)

		got, err := svc.Reveal(context.Background())
		if err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if got != key {
			t.Fatalf("key = %q, want %q", got, key)
		}
	})

	// An unconfigured installation is the expected case, not an error: the installer
	// binds a sentinel and the agent's tool reports itself unavailable.
	t.Run("empty and no error when unconfigured", func(t *testing.T) {
		svc, _ := newService(t, "")

		got, err := svc.Reveal(context.Background())
		if err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if got != "" {
			t.Fatalf("key = %q, want empty", got)
		}
	})
}
