package kv

import (
	"bytes"
	"context"
	"errors"
	"testing"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

// key32 is a fixed 32-byte (AES-256) key for these tests.
var key32 = bytes.Repeat([]byte{0x42}, 32)

// fakeRepo records the last value written so encryption behavior can be checked
// without a database.
type fakeRepo struct {
	stored   []byte
	version  int64
	writeErr error
}

func (f *fakeRepo) Get(context.Context, string, string, string) ([]byte, int64, bool, error) {
	if f.stored == nil {
		return nil, 0, false, nil
	}
	return f.stored, f.version, true, nil
}

func (f *fakeRepo) Write(_ context.Context, _, _, _ string, value []byte, _ int64) (int64, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	f.stored = value
	f.version++
	return f.version, nil
}

func (f *fakeRepo) List(context.Context, string, string) ([]Entry, error)       { return nil, nil }
func (f *fakeRepo) ListNamespaces(context.Context, string) ([]string, error)    { return nil, nil }
func (f *fakeRepo) Delete(context.Context, string, string, string, int64) error { return nil }
func (f *fakeRepo) DeleteByDeployment(context.Context, string) error            { return nil }

func testCipher(t *testing.T) *cryptox.Cipher {
	t.Helper()
	c, err := cryptox.NewCipher(key32)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func TestServicePlainNamespaceNotEncrypted(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil, testCipher(t))
	if _, err := svc.Set(context.Background(), "dep", "user", "k", []byte("plain"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if string(repo.stored) != "plain" {
		t.Fatalf("plain namespace stored %q, want it as-is", repo.stored)
	}
}

func TestServiceSecretNamespaceEncryptedAndDecrypted(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo, nil, testCipher(t))
	ctx := context.Background()

	if _, err := svc.Set(ctx, "dep", "system_secrets", "token", []byte("s3cr3t"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if string(repo.stored) == "s3cr3t" {
		t.Fatal("secret namespace stored as plaintext")
	}
	got, _, ok, err := svc.Get(ctx, "dep", "system_secrets", "token")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if string(got) != "s3cr3t" {
		t.Fatalf("Get = %q, want \"s3cr3t\"", got)
	}
}

func TestServiceSecretRejectedWithoutKey(t *testing.T) {
	svc := NewService(&fakeRepo{}, nil, nil) // no cipher
	if _, err := svc.Set(context.Background(), "dep", "user_secrets", "k", []byte("x"), 0); !errors.Is(err, ErrEncryptionDisabled) {
		t.Fatalf("secret Set without key: err = %v, want ErrEncryptionDisabled", err)
	}
	// A plain namespace still works without a key.
	if _, err := svc.Set(context.Background(), "dep", "user", "k", []byte("x"), 0); err != nil {
		t.Fatalf("plain Set without key: %v", err)
	}
}

func TestServiceConflictPassthrough(t *testing.T) {
	repo := &fakeRepo{writeErr: ErrVersionConflict}
	svc := NewService(repo, nil, testCipher(t))
	if _, err := svc.Set(context.Background(), "dep", "user", "k", []byte("x"), 5); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("err = %v, want ErrVersionConflict", err)
	}
}

func TestNamespaceTierClassification(t *testing.T) {
	cases := []struct {
		namespace string
		secret    bool
		volatile  bool
	}{
		{"user", false, false},
		{"system", false, false},
		{"user_secrets", true, false},
		{"system_secrets", true, false},
		{"user_volatile", false, true},
		{"system_volatile", false, true},
	}
	for _, tc := range cases {
		if got := isSecret(tc.namespace); got != tc.secret {
			t.Errorf("isSecret(%q) = %v, want %v", tc.namespace, got, tc.secret)
		}
		if got := isVolatile(tc.namespace); got != tc.volatile {
			t.Errorf("isVolatile(%q) = %v, want %v", tc.namespace, got, tc.volatile)
		}
		if err := checkTiers(tc.namespace); err != nil {
			t.Errorf("checkTiers(%q) = %v, want nil", tc.namespace, err)
		}
	}
}

// A namespace that is both secret and volatile asks the store to hold a credential
// somewhere it neither encrypts nor promises to keep. Nothing in the runtime
// composes the two; this is what stops a hand-written API call from doing it.
func TestServiceRefusesASecretVolatileNamespace(t *testing.T) {
	svc := NewService(&fakeRepo{}, nil, testCipher(t))
	ctx := context.Background()

	// Both composition orders, because only one suffix can be last: neither
	// "user_secrets_volatile" nor "user_volatile_secrets" is caught by looking at
	// the trailing suffix alone.
	for _, namespace := range []string{"user_secrets_volatile", "user_volatile_secrets"} {
		if _, err := svc.Set(ctx, "dep-1", namespace, "k", []byte("v"), 0); !errors.Is(err, ErrSecretNotVolatile) {
			t.Errorf("Set(%q) = %v, want ErrSecretNotVolatile", namespace, err)
		}
		if _, _, _, err := svc.Get(ctx, "dep-1", namespace, "k"); !errors.Is(err, ErrSecretNotVolatile) {
			t.Errorf("Get(%q) = %v, want ErrSecretNotVolatile", namespace, err)
		}
	}
}
