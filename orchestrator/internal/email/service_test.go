package email

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	cryptox "github.com/juancavallotti/octo/orchestrator/internal/crypto"
	"github.com/juancavallotti/octo/orchestrator/internal/mailer"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// fakeRepo holds one row in memory and counts writes, so a test can assert that a
// rejected request wrote nothing.
type fakeRepo struct {
	row      stored
	putCalls int
	getErr   error
}

func (f *fakeRepo) Get(context.Context) (stored, error) {
	return f.row, f.getErr
}

func (f *fakeRepo) Mutate(_ context.Context, fn func(stored) (stored, error)) error {
	next, err := fn(f.row)
	if err != nil {
		return err
	}
	f.putCalls++
	f.row = next
	return nil
}

// fakeSender records the last message and can be told to fail.
type fakeSender struct {
	last  mailer.Message
	calls int
	err   error
}

func (f *fakeSender) Send(_ context.Context, m mailer.Message) (string, error) {
	f.calls++
	f.last = m
	if f.err != nil {
		return "", f.err
	}
	return "msg-123", nil
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

// newService wires a service over fakes, with a row already configured when key is
// non-empty.
func newService(t *testing.T, key string) (*Service, *fakeRepo, *fakeSender) {
	t.Helper()
	c := testCipher(t)
	repo := &fakeRepo{row: stored{
		Provider:  providerResend,
		FromEmail: "notifications@acme.com",
		FromName:  "Acme",
		ReplyTo:   "support@acme.com",
	}}
	if key != "" {
		field, err := sitesettings.SecretField{}.Apply(&key, c)
		if err != nil {
			t.Fatalf("seed key: %v", err)
		}
		repo.row.APIKey = field
	}
	sender := &fakeSender{}
	return NewService(repo, sender, c), repo, sender
}

func TestUpdateValidation(t *testing.T) {
	valid := Update{FromEmail: "a@b.co", FromName: "Acme", ReplyTo: "r@b.co"}

	tests := []struct {
		name   string
		mutate func(*Update)
		want   error
	}{
		{"empty from", func(u *Update) { u.FromEmail = "" }, ErrInvalidFromEmail},
		{"malformed from", func(u *Update) { u.FromEmail = "not-an-email" }, ErrInvalidFromEmail},
		{
			// The display name belongs in fromName. Accepting it here too is how the
			// two end up disagreeing about who the mail is from.
			"from with display name",
			func(u *Update) { u.FromEmail = "Acme <a@b.co>" },
			ErrInvalidFromEmail,
		},
		{"malformed reply-to", func(u *Update) { u.ReplyTo = "nope" }, ErrInvalidReplyTo},
		{"over-long name", func(u *Update) { u.FromName = strings.Repeat("x", maxNameLen+1) }, ErrInvalidFromName},
		{"name with newline", func(u *Update) { u.FromName = "Acme\r\nBcc: x@y.z" }, ErrInvalidFromName},
		{"short api key", func(u *Update) { u.APIKey = ptr("re_abc") }, ErrInvalidAPIKey},
		{"over-long api key", func(u *Update) { u.APIKey = ptr(strings.Repeat("k", maxAPIKeyLen+1)) }, ErrInvalidAPIKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _ := newService(t, "")
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

// An empty reply-to is a legitimate value, not a malformed address.
func TestUpdateAllowsEmptyReplyTo(t *testing.T) {
	svc, _, _ := newService(t, "")

	got, err := svc.Update(context.Background(), Update{FromEmail: "a@b.co"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.ReplyTo != "" {
		t.Fatalf("replyTo = %q, want empty", got.ReplyTo)
	}
}

// The regression test for the whole design. The settings form submits every field on
// every save; if this breaks, changing a display name destroys the API key.
func TestUpdateWithoutAPIKeyPreservesStoredKey(t *testing.T) {
	svc, repo, _ := newService(t, "re_storedkey123")
	before := repo.row.APIKey

	got, err := svc.Update(context.Background(), Update{
		FromEmail: "new@acme.com",
		FromName:  "New Name",
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
	if repo.row.FromEmail != "new@acme.com" {
		t.Fatalf("fromEmail = %q, want it updated", repo.row.FromEmail)
	}
}

func TestUpdateReplacesAndClearsKey(t *testing.T) {
	t.Run("replace", func(t *testing.T) {
		svc, repo, _ := newService(t, "re_oldkey12345")

		got, err := svc.Update(context.Background(), Update{
			FromEmail: "a@b.co",
			APIKey:    ptr("re_newkey6789"),
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
		if revealed != "re_newkey6789" {
			t.Fatalf("stored key = %q, want the new one", revealed)
		}
	})

	t.Run("clear", func(t *testing.T) {
		svc, repo, _ := newService(t, "re_oldkey12345")

		got, err := svc.Update(context.Background(), Update{
			FromEmail: "a@b.co",
			APIKey:    ptr(""),
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if got.Configured || repo.row.APIKey.Configured() {
			t.Fatal("key should be gone after being cleared")
		}
	})
}

func TestUpdateWithoutCipher(t *testing.T) {
	t.Run("rejects a new key", func(t *testing.T) {
		repo := &fakeRepo{}
		svc := NewService(repo, &fakeSender{}, nil)

		_, err := svc.Update(context.Background(), Update{
			FromEmail: "a@b.co",
			APIKey:    ptr("re_abcdefgh1234"),
		})
		if !errors.Is(err, sitesettings.ErrEncryptionUnavailable) {
			t.Fatalf("err = %v, want ErrEncryptionUnavailable", err)
		}
		if repo.putCalls != 0 {
			t.Fatalf("putCalls = %d, want 0", repo.putCalls)
		}
	})

	// The other half: without an encryption key you can still correct the address
	// the mail goes out from, because carrying the stored key forward decrypts nothing.
	t.Run("still saves the other fields", func(t *testing.T) {
		repo := &fakeRepo{row: stored{FromEmail: "old@acme.com"}}
		svc := NewService(repo, &fakeSender{}, nil)

		if _, err := svc.Update(context.Background(), Update{FromEmail: "new@acme.com"}); err != nil {
			t.Fatalf("Update: %v", err)
		}
		if repo.row.FromEmail != "new@acme.com" {
			t.Fatalf("fromEmail = %q", repo.row.FromEmail)
		}
	})
}

func TestSendTestPrefersSuppliedKey(t *testing.T) {
	svc, _, sender := newService(t, "re_storedkey123")

	if _, err := svc.SendTest(context.Background(), TestSend{
		To:        "me@example.com",
		APIKey:    ptr("re_typedkey456"),
		FromEmail: "notifications@acme.com",
	}); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if sender.last.APIKey != "re_typedkey456" {
		t.Fatalf("key = %q, want the supplied one — testing before saving is the point", sender.last.APIKey)
	}
}

func TestSendTestFallsBackToStoredKey(t *testing.T) {
	svc, _, sender := newService(t, "re_storedkey123")

	if _, err := svc.SendTest(context.Background(), TestSend{
		To:        "me@example.com",
		FromEmail: "notifications@acme.com",
	}); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if sender.last.APIKey != "re_storedkey123" {
		t.Fatalf("key = %q, want the stored one", sender.last.APIKey)
	}
}

func TestSendTestWithoutAnyKey(t *testing.T) {
	svc, _, sender := newService(t, "")

	_, err := svc.SendTest(context.Background(), TestSend{
		To:        "me@example.com",
		FromEmail: "notifications@acme.com",
	})
	if !errors.Is(err, sitesettings.ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if sender.calls != 0 {
		t.Fatalf("sender called %d times, want 0", sender.calls)
	}
}

func TestSendTestRejectsBadRecipient(t *testing.T) {
	svc, _, sender := newService(t, "re_storedkey123")

	_, err := svc.SendTest(context.Background(), TestSend{To: "nope", FromEmail: "a@b.co"})
	if !errors.Is(err, ErrInvalidRecipient) {
		t.Fatalf("err = %v, want ErrInvalidRecipient", err)
	}
	if sender.calls != 0 {
		t.Fatalf("sender called %d times, want 0", sender.calls)
	}
}

// The test send uses the draft on screen, not what is stored — otherwise it verifies
// a from-address the operator is not looking at.
func TestSendTestUsesTheDraftIdentity(t *testing.T) {
	svc, _, sender := newService(t, "re_storedkey123")

	if _, err := svc.SendTest(context.Background(), TestSend{
		To:        "me@example.com",
		FromEmail: "draft@acme.com",
		FromName:  "Draft Name",
	}); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if sender.last.From != `"Draft Name" <draft@acme.com>` {
		t.Fatalf("from = %q, want the draft identity", sender.last.From)
	}
}

func TestFromComposition(t *testing.T) {
	tests := []struct {
		name string
		row  stored
		want string
	}{
		{"bare address without a name", stored{FromEmail: "a@b.co"}, "a@b.co"},
		{"quoted pair with a name", stored{FromEmail: "a@b.co", FromName: "Acme"}, `"Acme" <a@b.co>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.row.from(); got != tc.want {
				t.Fatalf("from() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSendValidation(t *testing.T) {
	valid := SendRequest{To: []string{"a@b.co"}, Subject: "hi", Text: "body"}

	tests := []struct {
		name   string
		mutate func(*SendRequest)
		want   error
	}{
		{"no recipients", func(r *SendRequest) { r.To = nil }, ErrInvalidRecipients},
		{
			"too many recipients",
			func(r *SendRequest) {
				r.To = make([]string, maxRecipients+1)
				for i := range r.To {
					r.To[i] = "a@b.co"
				}
			},
			ErrInvalidRecipients,
		},
		{
			"one bad address among good ones",
			func(r *SendRequest) { r.To = []string{"a@b.co", "nope", "c@d.co"} },
			ErrInvalidRecipients,
		},
		{"empty subject", func(r *SendRequest) { r.Subject = "  " }, ErrInvalidSubject},
		{"over-long subject", func(r *SendRequest) { r.Subject = strings.Repeat("x", maxSubjectLen+1) }, ErrInvalidSubject},
		{
			// Header injection: a newline in the subject is an attempt to add headers.
			"subject with a newline",
			func(r *SendRequest) { r.Subject = "Hi\r\nBcc: attacker@evil.com" },
			ErrInvalidSubject,
		},
		{"no body at all", func(r *SendRequest) { r.Text = ""; r.HTML = "" }, ErrEmptyBody},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, sender := newService(t, "re_storedkey123")
			req := valid
			tc.mutate(&req)

			if _, err := svc.Send(context.Background(), req); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if sender.calls != 0 {
				t.Fatalf("sender called %d times, want 0 — validation happens before the provider", sender.calls)
			}
		})
	}
}

func TestSendAcceptsHTMLOnly(t *testing.T) {
	svc, _, sender := newService(t, "re_storedkey123")

	if _, err := svc.Send(context.Background(), SendRequest{
		To:      []string{"a@b.co"},
		Subject: "hi",
		HTML:    "<p>hello</p>",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sender.last.HTML != "<p>hello</p>" || sender.last.Text != "" {
		t.Fatalf("message = %+v", sender.last)
	}
}

// Send takes its identity and key from storage and nowhere else — there is no request
// field through which a caller could supply either.
func TestSendUsesStoredIdentityAndKey(t *testing.T) {
	svc, _, sender := newService(t, "re_storedkey123")

	id, err := svc.Send(context.Background(), SendRequest{
		To:      []string{"a@b.co"},
		Subject: "hi",
		Text:    "body",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "msg-123" {
		t.Fatalf("id = %q", id)
	}
	if sender.last.APIKey != "re_storedkey123" {
		t.Fatalf("key = %q, want the stored one", sender.last.APIKey)
	}
	if sender.last.From != `"Acme" <notifications@acme.com>` {
		t.Fatalf("from = %q, want the stored identity", sender.last.From)
	}
	if sender.last.ReplyTo != "support@acme.com" {
		t.Fatalf("replyTo = %q, want the stored one", sender.last.ReplyTo)
	}
}

// Validation trims before parsing, so a padded address passes. What reaches the
// provider must be trimmed too, or our own check would pass and theirs would fail.
func TestSendNormalizesAddressesAndSubject(t *testing.T) {
	svc, _, sender := newService(t, "re_storedkey123")

	if _, err := svc.Send(context.Background(), SendRequest{
		To:      []string{" a@b.co ", "\tc@d.co\n"},
		Subject: "  hi  ",
		Text:    "  body keeps its spacing  ",
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sender.last.To[0] != "a@b.co" || sender.last.To[1] != "c@d.co" {
		t.Fatalf("to = %q, want trimmed addresses", sender.last.To)
	}
	if sender.last.Subject != "hi" {
		t.Fatalf("subject = %q, want trimmed", sender.last.Subject)
	}
	if sender.last.Text != "  body keeps its spacing  " {
		t.Fatalf("text = %q, want the body left alone", sender.last.Text)
	}
}

func TestSendWhenNotConfigured(t *testing.T) {
	tests := []struct {
		name string
		row  stored
	}{
		{"nothing stored at all", stored{}},
		{"identity but no key", stored{FromEmail: "a@b.co"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeRepo{row: tc.row}
			sender := &fakeSender{}
			svc := NewService(repo, sender, testCipher(t))

			_, err := svc.Send(context.Background(), SendRequest{
				To:      []string{"a@b.co"},
				Subject: "hi",
				Text:    "body",
			})
			if !errors.Is(err, sitesettings.ErrNotConfigured) {
				t.Fatalf("err = %v, want ErrNotConfigured", err)
			}
			if sender.calls != 0 {
				t.Fatalf("sender called %d times, want 0", sender.calls)
			}
		})
	}
}

// The provider's sentinels must survive the service boundary, or the handler cannot
// tell a bad key from an internal failure.
func TestSendPropagatesProviderErrors(t *testing.T) {
	for _, want := range []error{mailer.ErrInvalidAPIKey, mailer.ErrRejected, mailer.ErrUnavailable} {
		t.Run(want.Error(), func(t *testing.T) {
			svc, _, sender := newService(t, "re_storedkey123")
			sender.err = want

			_, err := svc.Send(context.Background(), SendRequest{
				To:      []string{"a@b.co"},
				Subject: "hi",
				Text:    "body",
			})
			if !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
		})
	}
}
