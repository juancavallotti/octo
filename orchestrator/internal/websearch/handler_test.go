package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestHandlerGetOnEmptyStore(t *testing.T) {
	svc := NewService(&fakeRepo{}, testCipher(t))
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodGet, url+"/settings/websearch", "")
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
	if got.Provider != Provider {
		t.Fatalf("provider = %q, want %q", got.Provider, Provider)
	}
	if got.UpdatedAt != nil {
		t.Fatalf("updatedAt = %v, want null", got.UpdatedAt)
	}
	if !got.EncryptionAvailable {
		t.Fatal("encryptionAvailable = false with a cipher configured")
	}
}

// The strongest form of "the key is never returned": the bytes on the wire do not
// contain it.
func TestHandlerUpdateNeverEchoesTheKey(t *testing.T) {
	svc, _ := newService(t, "")
	url := newTestServer(t, svc)

	const key = "parallel-averysecretkey9f2a"
	status, body := do(t, http.MethodPut, url+"/settings/websearch", `{"apiKey":"`+key+`"}`)
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

func TestHandlerUpdateRejectsUnknownField(t *testing.T) {
	svc, _ := newService(t, "")
	url := newTestServer(t, svc)

	status, _ := do(t, http.MethodPut, url+"/settings/websearch", `{"surprise":true}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
}

func TestHandlerUpdateInvalidKey(t *testing.T) {
	svc, _ := newService(t, "")
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodPut, url+"/settings/websearch", `{"apiKey":"nope"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
	if !strings.Contains(body, "api key") {
		t.Fatalf("body = %s, want it to name the field", body)
	}
}

// Without an encryption key the save is refused rather than performed in the clear,
// and the message names the setting an operator has to change.
func TestHandlerUpdateWithoutCipher(t *testing.T) {
	svc := NewService(&fakeRepo{}, nil)
	url := newTestServer(t, svc)

	status, body := do(t, http.MethodPut, url+"/settings/websearch", `{"apiKey":"parallel-abcdefgh1234"}`)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
	if !strings.Contains(body, "KV_ENCRYPTION_KEY") {
		t.Fatalf("body = %s, want it to name the env var", body)
	}
}

func TestHandlerGetReportsEncryptionUnavailable(t *testing.T) {
	svc := NewService(&fakeRepo{}, nil)
	url := newTestServer(t, svc)

	_, body := do(t, http.MethodGet, url+"/settings/websearch", "")
	if !strings.Contains(body, `"encryptionAvailable":false`) {
		t.Fatalf("body = %s, want encryptionAvailable false", body)
	}
}

// Guard against the settings response type drifting into carrying key material.
func TestSettingsResponseHasNoKeyField(t *testing.T) {
	c := testCipher(t)
	key := "parallel-averysecretkey"
	field, err := sitesettings.SecretField{}.Apply(&key, c)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	url := newTestServer(t, NewService(&fakeRepo{row: stored{APIKey: field}}, c))

	_, body := do(t, http.MethodGet, url+"/settings/websearch", "")

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

// There is no route that reveals the key, and none that reaches Parallel.
func TestNoRevealRoute(t *testing.T) {
	svc, _ := newService(t, "parallel-storedkey123")
	url := newTestServer(t, svc)

	for _, path := range []string{"/settings/websearch/reveal", "/settings/websearch/key", "/settings/websearch/test"} {
		status, _ := do(t, http.MethodPost, url+path, `{}`)
		if status != http.StatusNotFound {
			t.Fatalf("POST %s = %d, want 404", path, status)
		}
	}
}
