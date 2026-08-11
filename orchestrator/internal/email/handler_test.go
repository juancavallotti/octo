package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/mailer"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// newTestServer stands up the routes over the given service and returns its base URL.
func newTestServer(t *testing.T, svc *Service) string {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// do performs one request and returns the status and raw body.
func do(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, string(raw)
}

// configuredService returns a service whose row is fully set up, plus its fakes.
func configuredService(t *testing.T) (*Service, *fakeSender) {
	t.Helper()
	svc, _, sender := newService(t, "re_storedkey123")
	return svc, sender
}

func TestHandlerGetOnEmptyStore(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSender{}, testCipher(t))
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodGet, url+"/settings/email", "")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 — an unconfigured site is a state, not a 404", status)
	}
	var got settingsResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Configured {
		t.Fatal("configured = true on an empty store")
	}
	if got.UpdatedAt != nil {
		t.Fatalf("updatedAt = %v, want null", got.UpdatedAt)
	}
	if !got.EncryptionAvailable {
		t.Fatal("encryptionAvailable = false with a cipher configured")
	}
}

// The strongest form of "the key is never returned": the bytes on the wire do not
// contain it, whatever the response shape happens to be.
func TestHandlerUpdateNeverEchoesTheKey(t *testing.T) {
	svc, _ := configuredService(t)
	url := newTestServer(t, svc)

	const key = "re_averysecretkey9f2a"
	status, body := do(t, http.MethodPut, url+"/settings/email",
		`{"apiKey":"`+key+`","fromEmail":"a@b.co","fromName":"Acme","replyTo":""}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if strings.Contains(body, key) {
		t.Fatalf("response contains the api key: %s", body)
	}
	if !strings.Contains(body, `"last4":"9f2a"`) {
		t.Fatalf("response missing last4: %s", body)
	}
}

// DisallowUnknownFields is what enforces the contracts below, so it is worth pinning
// that it is actually on for this shape.
func TestHandlerUpdateRejectsUnknownField(t *testing.T) {
	svc, _ := configuredService(t)
	url := newTestServer(t, svc)

	status, _ := do(t, http.MethodPut, url+"/settings/email",
		`{"fromEmail":"a@b.co","surprise":true}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
}

func TestHandlerUpdateInvalidFromAddress(t *testing.T) {
	svc, _ := configuredService(t)
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodPut, url+"/settings/email", `{"fromEmail":"nope"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "from address") {
		t.Fatalf("body = %s, want it to name the field", body)
	}
}

// Without an encryption key the save is refused rather than performed in the clear,
// and the message names the setting an operator has to change.
func TestHandlerUpdateWithoutCipher(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSender{}, nil)
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodPut, url+"/settings/email",
		`{"apiKey":"re_abcdefgh1234","fromEmail":"a@b.co"}`)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
	if !strings.Contains(body, "KV_ENCRYPTION_KEY") {
		t.Fatalf("body = %s, want it to name the env var", body)
	}
}

func TestHandlerGetReportsEncryptionUnavailable(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSender{}, nil)
	url := newTestServer(t, svc)

	_, body := do(t, http.MethodGet, url+"/settings/email", "")
	if !strings.Contains(body, `"encryptionAvailable":false`) {
		t.Fatalf("body = %s, want encryptionAvailable false", body)
	}
}

// A bad API key is the caller's to fix, so it must not arrive as "internal error" —
// which is what happens the moment these fall into writeError's default branch.
func TestHandlerTestSendSurfacesProviderErrors(t *testing.T) {
	tests := []struct {
		name        string
		providerErr error
		wantStatus  int
		wantBody    string
	}{
		{"bad key", mailer.ErrInvalidAPIKey, http.StatusBadRequest, "API key"},
		{"unverified domain", mailer.ErrRejected, http.StatusBadRequest, "rejected"},
		{"provider down", mailer.ErrUnavailable, http.StatusBadGateway, "unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, sender := configuredService(t)
			sender.err = tc.providerErr
			url := newTestServer(t, svc)

			status, body := do(t, http.MethodPost, url+"/settings/email/test",
				`{"to":"me@example.com","fromEmail":"a@b.co"}`)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", status, tc.wantStatus, body)
			}
			if !strings.Contains(body, tc.wantBody) {
				t.Fatalf("body = %s, want it to mention %q", body, tc.wantBody)
			}
			if strings.Contains(body, "internal error") {
				t.Fatalf("body = %s, must not read as an internal error", body)
			}
		})
	}
}

// The send endpoint has no apiKey field at all, and the decoder is what enforces it:
// a caller trying to supply one is rejected rather than silently ignored.
func TestHandlerSendRejectsASuppliedKey(t *testing.T) {
	svc, sender := configuredService(t)
	url := newTestServer(t, svc)

	status, _ := do(t, http.MethodPost, url+"/email/send",
		`{"to":["a@b.co"],"subject":"hi","text":"body","apiKey":"re_smuggled1234"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if sender.calls != 0 {
		t.Fatalf("sender called %d times, want 0", sender.calls)
	}
}

func TestHandlerSendRejectsASuppliedFrom(t *testing.T) {
	svc, sender := configuredService(t)
	url := newTestServer(t, svc)

	status, _ := do(t, http.MethodPost, url+"/email/send",
		`{"to":["a@b.co"],"subject":"hi","text":"body","fromEmail":"attacker@evil.com"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if sender.calls != 0 {
		t.Fatalf("sender called %d times, want 0", sender.calls)
	}
}

func TestHandlerSendSuccess(t *testing.T) {
	svc, sender := configuredService(t)
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodPost, url+"/email/send",
		`{"to":["a@b.co"],"subject":"hi","text":"body","html":""}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if !strings.Contains(body, `"messageId":"msg-123"`) {
		t.Fatalf("body = %s", body)
	}
	if sender.last.From != `"Acme" <notifications@acme.com>` {
		t.Fatalf("from = %q, want the stored identity", sender.last.From)
	}
}

func TestHandlerSendWhenNotConfigured(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeSender{}, testCipher(t))
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodPost, url+"/email/send",
		`{"to":["a@b.co"],"subject":"hi","text":"body"}`)
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409", status)
	}
	if !strings.Contains(body, "not configured") {
		t.Fatalf("body = %s", body)
	}
}

func TestHandlerSendHeaderInjectionIsRejected(t *testing.T) {
	svc, sender := configuredService(t)
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodPost, url+"/email/send",
		`{"to":["a@b.co"],"subject":"Hi\r\nBcc: attacker@evil.com","text":"body"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", status, body)
	}
	if sender.calls != 0 {
		t.Fatalf("sender called %d times, want 0", sender.calls)
	}
}

// Guard against the settings response type drifting into carrying key material.
func TestSettingsResponseHasNoKeyField(t *testing.T) {
	c := testCipher(t)
	key := "re_averysecretkey"
	field, err := sitesettings.SecretField{}.Apply(&key, c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	repo := &fakeRepo{row: stored{FromEmail: "a@b.co", APIKey: field}}
	svc := NewService(repo, &fakeSender{}, c)
	url := newTestServer(t, svc)

	_, body := do(t, http.MethodGet, url+"/settings/email", "")

	var generic map[string]any
	if err := json.Unmarshal([]byte(body), &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, banned := range []string{"apiKey", "ciphertext", "key"} {
		if _, ok := generic[banned]; ok {
			t.Fatalf("settings response carries a %q field: %s", banned, body)
		}
	}
}
