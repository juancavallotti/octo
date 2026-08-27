package embedding

import (
	"bytes"
	"context"
	"errors"
	"testing"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
)

// fakeRepo is the settings row, in memory.
type fakeRepo struct{ cur stored }

func (f *fakeRepo) Get(context.Context) (stored, error) { return f.cur, nil }

func (f *fakeRepo) Mutate(_ context.Context, fn func(stored) (stored, error)) error {
	next, err := fn(f.cur)
	if err != nil {
		return err
	}
	f.cur = next
	return nil
}

// testCipher returns a cipher so key handling can be exercised.
func testCipher(t *testing.T) *cryptox.Cipher {
	t.Helper()
	c, err := cryptox.NewCipher(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	return c
}

func ptr(s string) *string { return &s }

// TestUpdateKeepsTheKeyWhenOnlyTheModelChanges is the case that makes the form
// usable: correcting a model should not mean re-entering a credential.
func TestUpdateKeepsTheKeyWhenOnlyTheModelChanges(t *testing.T) {
	svc := NewService(&fakeRepo{}, testCipher(t), nil)
	ctx := context.Background()

	if _, err := svc.Update(ctx, Update{
		Provider: ProviderOpenAI, Model: "text-embedding-3-small", APIKey: ptr("sk-secret-key"),
	}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	got, err := svc.Update(ctx, Update{Provider: ProviderOpenAI, Model: "text-embedding-3-large"})
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if !got.Configured {
		t.Error("changing the model should not have discarded the key")
	}
	creds, err := svc.Reveal(ctx)
	if err != nil || creds.APIKey != "sk-secret-key" {
		t.Errorf("the stored key should still be usable, got %q (err=%v)", creds.APIKey, err)
	}
}

// TestUpdateClearsTheKeyWhenTheProviderChanges is the exception, and it has to
// be: a key authenticates against one provider, so carrying it forward leaves the
// settings reporting a credential that cannot work.
func TestUpdateClearsTheKeyWhenTheProviderChanges(t *testing.T) {
	svc := NewService(&fakeRepo{}, testCipher(t), nil)
	ctx := context.Background()

	if _, err := svc.Update(ctx, Update{
		Provider: ProviderOpenAI, Model: "text-embedding-3-small", APIKey: ptr("sk-secret-key"),
	}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	got, err := svc.Update(ctx, Update{Provider: ProviderGoogle, Model: "text-embedding-004"})
	if err != nil {
		t.Fatalf("switch provider: %v", err)
	}
	if got.Configured {
		t.Error("a key for the old provider should not be carried onto the new one")
	}
}

// TestUpdateRejectsAnthropic states the one provider that will never be here.
// Anthropic has no embeddings API, and it is in the llm list, so its absence
// needs to be a refusal rather than a silent omission.
func TestUpdateRejectsAnthropic(t *testing.T) {
	svc := NewService(&fakeRepo{}, testCipher(t), nil)
	_, err := svc.Update(context.Background(), Update{Provider: "ANTHROPIC", Model: "claude"})
	if !errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("want ErrInvalidProvider, got %v", err)
	}
}

// TestClearTurnsItOff checks that clearing is complete, so search falls back to
// text immediately.
func TestClearTurnsItOff(t *testing.T) {
	svc := NewService(&fakeRepo{}, testCipher(t), nil)
	ctx := context.Background()

	if _, err := svc.Update(ctx, Update{
		Provider: ProviderOpenAI, Model: "text-embedding-3-small", APIKey: ptr("sk-secret-key"),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := svc.Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	creds, err := svc.Reveal(ctx)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if creds.Configured() {
		t.Error("clearing should leave nothing usable")
	}
}

// TestUpdateWithoutEncryptionRefusesAKey checks a key is never stored in the
// clear — the settings still save, the credential does not.
func TestUpdateWithoutEncryptionRefusesAKey(t *testing.T) {
	svc := NewService(&fakeRepo{}, nil, nil)
	_, err := svc.Update(context.Background(), Update{
		Provider: ProviderOpenAI, Model: "text-embedding-3-small", APIKey: ptr("sk-secret-key"),
	})
	if err == nil {
		t.Fatal("storing a key with no cipher should be refused, not performed in the clear")
	}
	if svc.EncryptionAvailable() {
		t.Error("EncryptionAvailable should report the absence")
	}
}

// fakeCounter reports fixed backfill counts.
type fakeCounter struct{ embedded, pending int }

func (f fakeCounter) EmbeddingCounts(context.Context) (int, int, error) {
	return f.embedded, f.pending, nil
}

// TestStatusReportsTheBackfill covers the distinction the admin page exists to
// show: configured and "search is semantic" are not the same statement while a
// backlog is draining.
func TestStatusReportsTheBackfill(t *testing.T) {
	svc := NewService(&fakeRepo{}, testCipher(t), fakeCounter{embedded: 40, pending: 60})
	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Embedded != 40 || status.Pending != 60 {
		t.Errorf("counts did not reach the status: %+v", status)
	}
	if status.Settings.Dimensions != Dimensions {
		t.Errorf("the stored width should be reported, got %d", status.Settings.Dimensions)
	}
}

// TestRevealIsQuietWhenNothingIsConfigured is why Reveal distinguishes "nobody
// turned this on" from "something is wrong".
//
// It is called on every sweep tick and every search. If an unconfigured install
// reported a failure, every deployment that has never wanted embeddings would log
// a warning every minute forever — which trains everyone to ignore the one that
// matters.
func TestRevealIsQuietWhenNothingIsConfigured(t *testing.T) {
	svc := NewService(&fakeRepo{}, testCipher(t), nil)
	creds, err := svc.Reveal(context.Background())
	if err != nil {
		t.Fatalf("an unconfigured install is not a failure: %v", err)
	}
	if creds.Configured() {
		t.Error("nothing is configured, so the credentials should not claim to be")
	}
}
