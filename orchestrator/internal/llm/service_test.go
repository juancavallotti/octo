package llm

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
	repo := &fakeRepo{row: stored{Provider: ProviderAnthropic, Model: "claude-sonnet-4-6"}}
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
	valid := Update{Provider: ProviderAnthropic, Model: "claude-sonnet-4-6"}

	tests := []struct {
		name   string
		mutate func(*Update)
		want   error
	}{
		{"empty provider", func(u *Update) { u.Provider = "" }, ErrInvalidProvider},
		{"unknown provider", func(u *Update) { u.Provider = "COHERE" }, ErrInvalidProvider},
		{
			// The stored form is the runtime's vocabulary, which is upper case.
			"lowercase provider",
			func(u *Update) { u.Provider = "anthropic" },
			ErrInvalidProvider,
		},
		{"empty model", func(u *Update) { u.Model = "  " }, ErrInvalidModel},
		{"over-long model", func(u *Update) { u.Model = strings.Repeat("x", maxModelLen+1) }, ErrInvalidModel},
		{"model with a newline", func(u *Update) { u.Model = "gpt\n5.4" }, ErrInvalidModel},
		{"short api key", func(u *Update) { u.APIKey = ptr("sk-abc") }, ErrInvalidAPIKey},
		{"over-long api key", func(u *Update) { u.APIKey = ptr(strings.Repeat("k", maxAPIKeyLen+1)) }, ErrInvalidAPIKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newService(t, "")
			u := valid
			tc.mutate(&u)

			if _, err := svc.Update(context.Background(), u); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if repo.putCalls != 0 {
				t.Fatalf("putCalls = %d, want 0 — a rejected save must not write", repo.putCalls)
			}
		})
	}
}

func TestUpdateAcceptsEveryProvider(t *testing.T) {
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			svc, _ := newService(t, "")

			got, err := svc.Update(context.Background(), Update{Provider: provider, Model: "some-model"})
			if err != nil {
				t.Fatalf("Update: %v", err)
			}
			if got.Provider != provider {
				t.Fatalf("provider = %q, want %q", got.Provider, provider)
			}
		})
	}
}

// The same invariant email has: switching provider or model must not destroy the
// credentials sitting beside them on the form.
func TestUpdateWithoutAPIKeyPreservesStoredKey(t *testing.T) {
	svc, repo := newService(t, "sk-ant-storedkey123")
	before := repo.row.APIKey

	got, err := svc.Update(context.Background(), Update{
		Provider: ProviderOpenAI,
		Model:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !bytes.Equal(repo.row.APIKey.Ciphertext, before.Ciphertext) {
		t.Fatal("stored ciphertext changed on a save that did not supply a key")
	}
	if !got.Configured || got.Last4 != before.Last4 {
		t.Fatalf("settings = %+v, want the key still reported as stored", got)
	}
	if repo.row.Provider != ProviderOpenAI || repo.row.Model != "gpt-5.4" {
		t.Fatalf("row = %+v, want provider and model updated", repo.row)
	}
}

func TestUpdateReplacesAndClearsKey(t *testing.T) {
	t.Run("replace", func(t *testing.T) {
		svc, repo := newService(t, "sk-oldkey12345")

		got, err := svc.Update(context.Background(), Update{
			Provider: ProviderAnthropic,
			Model:    "claude-sonnet-4-6",
			APIKey:   ptr("sk-ant-newkey6789"),
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.Last4 != "6789" {
			t.Fatalf("last4 = %q, want %q", got.Last4, "6789")
		}
		revealed, err := repo.row.APIKey.Reveal(testCipher(t))
		if err != nil {
			t.Fatalf("Reveal: %v", err)
		}
		if revealed != "sk-ant-newkey6789" {
			t.Fatalf("stored key = %q, want the new one", revealed)
		}
	})

	t.Run("clear", func(t *testing.T) {
		svc, repo := newService(t, "sk-oldkey12345")

		got, err := svc.Update(context.Background(), Update{
			Provider: ProviderAnthropic,
			Model:    "claude-sonnet-4-6",
			APIKey:   ptr(""),
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.Configured || repo.row.APIKey.Configured() {
			t.Fatal("key should be gone after being cleared")
		}
	})
}

func TestUpdateTrimsModel(t *testing.T) {
	svc, repo := newService(t, "")

	if _, err := svc.Update(context.Background(), Update{
		Provider: ProviderGoogle,
		Model:    "  gemini-3.5-flash  ",
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if repo.row.Model != "gemini-3.5-flash" {
		t.Fatalf("model = %q, want it trimmed", repo.row.Model)
	}
}

func TestUpdateWithoutCipher(t *testing.T) {
	t.Run("rejects a new key", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo, nil)

		_, err := svc.Update(context.Background(), Update{
			Provider: ProviderAnthropic,
			Model:    "claude-sonnet-4-6",
			APIKey:   ptr("sk-ant-abcdefgh1234"),
		})
		if !errors.Is(err, sitesettings.ErrEncryptionUnavailable) {
			t.Fatalf("err = %v, want ErrEncryptionUnavailable", err)
		}
		if repo.putCalls != 0 {
			t.Fatalf("putCalls = %d, want 0", repo.putCalls)
		}
	})

	t.Run("still saves provider and model", func(t *testing.T) {
		repo := &fakeRepo{row: stored{Provider: ProviderAnthropic, Model: "old-model"}}
		svc := NewService(repo, nil)

		if _, err := svc.Update(context.Background(), Update{
			Provider: ProviderOpenAI,
			Model:    "gpt-5.4",
		}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if repo.row.Model != "gpt-5.4" {
			t.Fatalf("model = %q", repo.row.Model)
		}
	})
}

// Reveal is what the agent work will call. It is the only path to the key, and it
// is not reachable over HTTP.
func TestRevealReturnsCredentials(t *testing.T) {
	svc, _ := newService(t, "sk-ant-storedkey123")

	got, err := svc.Reveal(context.Background())
	if err != nil {
		t.Fatalf("Reveal: %v", err)
	}
	if got.APIKey != "sk-ant-storedkey123" {
		t.Fatalf("apiKey = %q", got.APIKey)
	}
	if got.Provider != ProviderAnthropic || got.Model != "claude-sonnet-4-6" {
		t.Fatalf("credentials = %+v", got)
	}
}

func TestRevealWhenNotConfigured(t *testing.T) {
	svc, _ := newService(t, "")

	if _, err := svc.Reveal(context.Background()); !errors.Is(err, sitesettings.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

func TestGetNeverCarriesTheKey(t *testing.T) {
	svc, _ := newService(t, "sk-ant-storedkey123")

	got, err := svc.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Configured {
		t.Fatal("configured = false with a key stored")
	}
	if got.Last4 != "y123" {
		t.Fatalf("last4 = %q", got.Last4)
	}
}
