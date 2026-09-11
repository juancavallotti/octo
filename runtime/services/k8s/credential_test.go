package k8s

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// staticCredential is a credential that holds one token and never renews it —
// what the tests of the clients around it want, since what they are checking is
// that the header goes out, not how it is kept fresh.
func staticCredential(token string) *credential {
	return &credential{token: token, expiresAt: time.Now().Add(time.Hour)}
}

// mintToken builds a token minted now whose `exp` is `in` from now. Unsigned:
// nothing here verifies it, and the point is the timestamps.
func mintToken(t *testing.T, in time.Duration) string {
	t.Helper()
	return mintTokenAt(t, time.Now(), in)
}

// mintTokenAt is mintToken with the issue time stated, for the orderings that
// turn on which of two tokens is newer.
func mintTokenAt(t *testing.T, issued time.Time, in time.Duration) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"iat": issued.Unix(), "exp": issued.Add(in).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"ES256"}`)) + "." + enc(payload) + "." + enc([]byte("signature"))
}

func TestNoCredentialSendsNoHeader(t *testing.T) {
	for name, c := range map[string]*credential{
		"nil":     nil,
		"unset":   newCredential(credentialConfig{}),
		"missing": newCredential(credentialConfig{Seed: filepath.Join(t.TempDir(), "absent")}),
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.invalid/", nil)
			c.authorize(req)
			if got := req.Header.Get("Authorization"); got != "" {
				t.Errorf("Authorization = %q, want none", got)
			}
		})
	}
}

func TestCredentialReadsTheMountedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	token := mintToken(t, time.Hour)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.invalid/", nil)
	newCredential(credentialConfig{Seed: path}).authorize(req)

	if got := req.Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization = %q, want the mounted token", got)
	}
}

// The file wins: a deployment gets one, and the variable is only what a local run
// can set instead.
func TestTheMountedFileWinsOverTheVariable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	mounted := mintToken(t, time.Hour)
	if err := os.WriteFile(path, []byte(mounted), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.invalid/", nil)
	newCredential(credentialConfig{Seed: path, Inline: "from-the-environment"}).authorize(req)

	if got := req.Header.Get("Authorization"); got != "Bearer "+mounted {
		t.Errorf("Authorization = %q, want the mounted token", got)
	}
}

func TestCredentialFallsBackToTheVariable(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.invalid/", nil)
	newCredential(credentialConfig{Inline: "inline-token"}).authorize(req)

	if got := req.Header.Get("Authorization"); got != "Bearer inline-token" {
		t.Errorf("Authorization = %q, want the inline token", got)
	}
}

func TestCredentialRenewsATokenNearExpiry(t *testing.T) {
	fresh := mintToken(t, time.Hour)
	var calls int
	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/auth/refresh" {
			t.Errorf("renewal went to %q, want /auth/refresh", r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("the renewal presented no credential")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": fresh})
	}))
	defer iam.Close()

	path := filepath.Join(t.TempDir(), "token")
	// A minute left, well inside the lead.
	if err := os.WriteFile(path, []byte(mintToken(t, time.Minute)), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c := newCredential(credentialConfig{Seed: path, IAMURL: iam.URL})

	if got := c.get(context.Background()); got != fresh {
		t.Errorf("get() = %q, want the renewed token", got)
	}
	// The renewed one has an hour on it, so a second call must not go back.
	if got := c.get(context.Background()); got != fresh {
		t.Errorf("get() = %q on the second call, want the renewed token", got)
	}
	if calls != 1 {
		t.Errorf("iam was asked %d times, want 1", calls)
	}
}

// A token the orchestrator has replaced on disk is cheaper and safer than one we
// renew ourselves, so the file is consulted first.
func TestCredentialPrefersAReplacedFileOverRenewing(t *testing.T) {
	iam := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("iam was asked to renew when the file already held a fresh token")
	}))
	defer iam.Close()

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(mintToken(t, time.Minute)), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c := newCredential(credentialConfig{Seed: path, IAMURL: iam.URL})

	replaced := mintToken(t, time.Hour)
	if err := os.WriteFile(path, []byte(replaced), 0o600); err != nil {
		t.Fatalf("replace token: %v", err)
	}
	if got := c.get(context.Background()); got != replaced {
		t.Errorf("get() = %q, want the replaced token", got)
	}
}

// iam being briefly unreachable is not a reason to start making unauthenticated
// requests: the token we hold may have minutes left on it.
func TestCredentialKeepsTheOldTokenWhenRenewalFails(t *testing.T) {
	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer iam.Close()

	path := filepath.Join(t.TempDir(), "token")
	held := mintToken(t, time.Minute)
	if err := os.WriteFile(path, []byte(held), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c := newCredential(credentialConfig{Seed: path, IAMURL: iam.URL})

	if got := c.get(context.Background()); got != held {
		t.Errorf("get() = %q, want the token we already had", got)
	}
}

// A credential must not print itself into a log line.
func TestCredentialDoesNotPrintItsToken(t *testing.T) {
	c := staticCredential("super-secret")
	if got := c.String(); got == "" || strings.Contains(got, "super-secret") {
		t.Errorf("String() = %q, and it should not carry the token", got)
	}
	if got := (*credential)(nil).String(); got != "no credential" {
		t.Errorf("nil String() = %q, want %q", got, "no credential")
	}
}

// The orchestrator can replace the mounted token — a rollout, a rotation, a
// revocation — and a pod that only looked when its own copy was expiring would
// keep presenting a withdrawn credential for the best part of an hour.
func TestCredentialPicksUpAReplacedFileWhileItsTokenIsStillGood(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	// Minted a minute ago, so the replacement below is unambiguously newer —
	// adoption turns on which token is more recent, and two minted in the same
	// second are not.
	original := mintTokenAt(t, time.Now().Add(-time.Minute), time.Hour)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c := newCredential(credentialConfig{Seed: path})

	if got := c.get(t.Context()); got != original {
		t.Fatalf("get() = %q, want the mounted token", got)
	}

	replaced := mintToken(t, 2*time.Hour)
	if err := os.WriteFile(path, []byte(replaced), 0o600); err != nil {
		t.Fatalf("replace token: %v", err)
	}
	if got := c.get(t.Context()); got != replaced {
		t.Errorf("get() = %q after the file was replaced, want the new token", got)
	}
}

// The other direction, and the one the obvious fix gets wrong: after a renewal
// the in-memory token is newer than the file, and re-adopting the file would undo
// it — then renew again on the next call, and the next, once per request.
func TestCredentialDoesNotUndoARenewalFromTheStaleFile(t *testing.T) {
	fresh := mintToken(t, time.Hour)
	var calls int
	iam := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]string{"token": fresh})
	}))
	defer iam.Close()

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(mintToken(t, time.Minute)), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	c := newCredential(credentialConfig{Seed: path, IAMURL: iam.URL})

	for range 5 {
		if got := c.get(t.Context()); got != fresh {
			t.Fatalf("get() = %q, want the renewed token", got)
		}
	}
	if calls != 1 {
		t.Errorf("iam was asked %d times across five calls, want 1", calls)
	}
}

// --- renewal at start ------------------------------------------------------

// fakeIAM answers a renewal with the given token and counts the calls.
func fakeIAM(t *testing.T, issue string, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls != nil {
			*calls++
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": issue})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeToken(t *testing.T, path, token string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// start spends the seed straight away rather than waiting for a request, so a pod
// is never serving on a token written before it existed.
func TestCredentialRenewsOnStart(t *testing.T) {
	fresh := mintToken(t, time.Hour)
	var calls int
	dir := t.TempDir()
	seed := filepath.Join(dir, "seed")
	// Hours left, so nothing on the demand path would ever renew it.
	writeToken(t, seed, mintToken(t, 3*time.Hour))

	c := newCredential(credentialConfig{
		Seed: seed, IAMURL: fakeIAM(t, fresh, &calls).URL,
	})
	c.start(context.Background())

	if calls != 1 {
		t.Errorf("iam was asked %d times at start, want 1", calls)
	}
	if got := c.get(context.Background()); got != fresh {
		t.Errorf("get() = %q, want the token renewed at start", got)
	}
}

// Nothing configured is a supported shape: start must be a no-op rather than a
// panic or a request.
func TestCredentialStartWithoutAnythingConfigured(t *testing.T) {
	newCredential(credentialConfig{}).start(context.Background())
	var absent *credential
	absent.start(context.Background())
}
