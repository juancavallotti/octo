package httpclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// fakeMetadata stands in for the instance metadata server. The metadata client
// honours GCE_METADATA_HOST, so pointing it at an httptest server exercises the
// real client, the real paths, and the real header contract without a network or
// a build tag.
type fakeMetadata struct {
	srv       *httptest.Server
	calls     atomic.Int64
	lastPath  atomic.Value // string
	lastQuery atomic.Value // string
	identity  string
	access    string
	status    int
}

func startMetadata(t *testing.T, m *fakeMetadata) *fakeMetadata {
	t.Helper()
	if m.status == 0 {
		m.status = http.StatusOK
	}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The client must identify itself, or a real metadata server refuses.
		if r.Header.Get("Metadata-Flavor") != "Google" {
			t.Errorf("request without Metadata-Flavor: Google")
		}
		m.calls.Add(1)
		m.lastPath.Store(r.URL.Path)
		m.lastQuery.Store(r.URL.RawQuery)

		if m.status != http.StatusOK {
			w.WriteHeader(m.status)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/identity") {
			_, _ = w.Write([]byte(m.identity))
			return
		}
		_, _ = w.Write([]byte(m.access))
	}))
	t.Cleanup(m.srv.Close)

	host := strings.TrimPrefix(m.srv.URL, "http://")
	t.Setenv("GCE_METADATA_HOST", host)
	return m
}

func (m *fakeMetadata) path() string  { v, _ := m.lastPath.Load().(string); return v }
func (m *fakeMetadata) query() string { v, _ := m.lastQuery.Load().(string); return v }

// signedJWT builds a token whose exp claim is `in` from now. Only the payload is
// meaningful — nothing verifies the signature, by design.
func signedJWT(in time.Duration) string {
	payload, _ := json.Marshal(map[string]any{"exp": time.Now().Add(in).Unix()})
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + "." + enc([]byte("signature"))
}

// accessTokenJSON is the shape the metadata server answers /token with.
func accessTokenJSON(token string, expiresIn int) string {
	return fmt.Sprintf(`{"access_token":%q,"expires_in":%d,"token_type":"Bearer"}`, token, expiresIn)
}

// gcpGet issues a request and returns the error rather than failing the test on
// it, which the package's own get helper does not — and the point of several
// cases below is exactly which error comes back.
func gcpGet(t *testing.T, c *Connector, path string) error {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// gcpConnector starts a connector with gcp auth pointed at the given upstream.
func gcpConnector(t *testing.T, baseURL string, auth map[string]any) *Connector {
	t.Helper()
	return startConnector(t, types.Settings{"baseURL": baseURL, "auth": auth})
}

func TestGCPIdentityTokenIsSentAsBearer(t *testing.T) {
	meta := startMetadata(t, &fakeMetadata{identity: signedJWT(time.Hour)})

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := gcpConnector(t, upstream.URL, map[string]any{"type": "gcp"})
	if err := gcpGet(t, conn, "/orders"); err != nil {
		t.Fatalf("request: %v", err)
	}

	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("Authorization = %q, want a bearer token", gotAuth)
	}
	if strings.TrimPrefix(gotAuth, "Bearer ") != meta.identity {
		t.Error("the token sent is not the one the metadata server minted")
	}
	if !strings.HasSuffix(meta.path(), "/identity") {
		t.Errorf("metadata path = %q, want the identity endpoint", meta.path())
	}
}

func TestGCPIdentityAudienceDefaultsToTheBaseURL(t *testing.T) {
	// The point of the default: calling one Cloud Run service from another needs
	// no audience configured, because the audience *is* the service being called.
	meta := startMetadata(t, &fakeMetadata{identity: signedJWT(time.Hour)})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := gcpConnector(t, upstream.URL+"/api/v1", map[string]any{"type": "gcp"})
	if err := gcpGet(t, conn, "/orders"); err != nil {
		t.Fatalf("request: %v", err)
	}

	// Origin only — the path a connector happens to be based at is not part of
	// the identity the receiver validates. Parse the query rather than matching an
	// escaped string, so the assertion is about the value and not the encoding.
	params, err := url.ParseQuery(meta.query())
	if err != nil {
		t.Fatalf("metadata query %q: %v", meta.query(), err)
	}
	if got := params.Get("audience"); got != upstream.URL {
		t.Errorf("audience = %q, want the base URL origin %q", got, upstream.URL)
	}
}

func TestGCPIdentityUsesAnExplicitAudience(t *testing.T) {
	meta := startMetadata(t, &fakeMetadata{identity: signedJWT(time.Hour)})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := gcpConnector(t, upstream.URL, map[string]any{
		"type": "gcp", "gcpAudience": "https://orders-abc.a.run.app",
	})
	if err := gcpGet(t, conn, "/x"); err != nil {
		t.Fatalf("request: %v", err)
	}
	if !strings.Contains(meta.query(), "orders-abc.a.run.app") {
		t.Errorf("audience query = %q, want the configured audience", meta.query())
	}
}

func TestGCPAccessTokenWithScopes(t *testing.T) {
	meta := startMetadata(t, &fakeMetadata{access: accessTokenJSON("test-access-token", 3600)})

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := gcpConnector(t, upstream.URL, map[string]any{
		"type":     "gcp",
		"gcpToken": "access",
		"gcpScopes": []any{
			"https://www.googleapis.com/auth/devstorage.read_only",
			"https://www.googleapis.com/auth/pubsub",
		},
	})
	if err := gcpGet(t, conn, "/b/bucket"); err != nil {
		t.Fatalf("request: %v", err)
	}

	if gotAuth != "Bearer test-access-token" {
		t.Errorf("Authorization = %q, want the access token", gotAuth)
	}
	if !strings.HasSuffix(meta.path(), "/token") {
		t.Errorf("metadata path = %q, want the token endpoint", meta.path())
	}
	// Scopes go as one comma-joined parameter, which is what the server expects.
	if !strings.Contains(meta.query(), "devstorage.read_only%2Chttps") {
		t.Errorf("scopes query = %q, want the scopes comma-joined", meta.query())
	}
}

func TestGCPCachesTheToken(t *testing.T) {
	meta := startMetadata(t, &fakeMetadata{identity: signedJWT(time.Hour)})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := gcpConnector(t, upstream.URL, map[string]any{"type": "gcp"})
	for range 3 {
		if err := gcpGet(t, conn, "/x"); err != nil {
			t.Fatalf("request: %v", err)
		}
	}
	if got := meta.calls.Load(); got != 1 {
		t.Errorf("metadata server called %d times for 3 requests, want 1", got)
	}
}

func TestGCPRefetchesAnExpiredToken(t *testing.T) {
	// An already-expired exp claim must not be served. The skew means a token
	// expiring within 30s counts as expired too.
	meta := startMetadata(t, &fakeMetadata{identity: signedJWT(-time.Minute)})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := gcpConnector(t, upstream.URL, map[string]any{"type": "gcp"})
	for range 2 {
		if err := gcpGet(t, conn, "/x"); err != nil {
			t.Fatalf("request: %v", err)
		}
	}
	if got := meta.calls.Load(); got != 2 {
		t.Errorf("metadata server called %d times, want 2 (the token was expired)", got)
	}
}

func TestIdentityExpiry(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  func(int64) bool
	}{
		{"reads the exp claim", signedJWT(time.Hour), func(got int64) bool {
			delta := time.Until(time.Unix(0, got))
			return delta > 59*time.Minute && delta <= time.Hour
		}},
		{"falls back on a non-JWT", "not-a-jwt", func(got int64) bool {
			return time.Until(time.Unix(0, got)) <= gcpFallbackTTL
		}},
		{"falls back on undecodable payload", "a.!!!.c", func(got int64) bool {
			return time.Until(time.Unix(0, got)) <= gcpFallbackTTL
		}},
		{"falls back when exp is absent", func() string {
			enc := base64.RawURLEncoding.EncodeToString
			return enc([]byte("{}")) + "." + enc([]byte(`{"sub":"x"}`)) + "." + enc([]byte("s"))
		}(), func(got int64) bool {
			return time.Until(time.Unix(0, got)) <= gcpFallbackTTL
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := identityExpiry(tc.token); !tc.want(got) {
				t.Errorf("identityExpiry(%.20q) = %v, outside the expected window", tc.token, got)
			}
		})
	}
}

func TestGCPMetadataFailureSurfaces(t *testing.T) {
	startMetadata(t, &fakeMetadata{status: http.StatusInternalServerError})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := gcpConnector(t, upstream.URL, map[string]any{"type": "gcp"})
	err := gcpGet(t, conn, "/x")
	if err == nil {
		t.Fatal("request succeeded, want an error")
	}
	// The error must name the scheme, not "oauth2" as it did before gcp existed.
	if !strings.Contains(err.Error(), "gcp auth") {
		t.Errorf("error %v does not name the gcp scheme", err)
	}
}

func TestGCPMetadataHonorsConnectorTimeout(t *testing.T) {
	// A hung metadata server must not hold the caller for longer than the
	// connector's timeout. Without the bound the call runs until the flow's own
	// deadline, which is a much later and much less predictable moment.
	meta := &fakeMetadata{identity: signedJWT(time.Hour)}
	startMetadata(t, meta)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()
	t.Setenv("GCE_METADATA_HOST", strings.TrimPrefix(slow.URL, "http://"))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	conn := startConnector(t, types.Settings{
		"baseURL": upstream.URL,
		"timeout": "200ms",
		"auth":    map[string]any{"type": "gcp"},
	})

	start := time.Now()
	err := gcpGet(t, conn, "/x")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("request succeeded against a hung metadata server, want a timeout")
	}
	if elapsed > time.Second {
		t.Errorf("waited %v for a 200ms timeout — the connector timeout was not applied", elapsed)
	}
}

func TestGCPStartValidation(t *testing.T) {
	cases := []struct {
		name    string
		auth    map[string]any
		wantErr bool
	}{
		{"bare gcp needs nothing else", map[string]any{"type": "gcp"}, false},
		{"identity", map[string]any{"type": "gcp", "gcpToken": "identity"}, false},
		{"access", map[string]any{"type": "gcp", "gcpToken": "access"}, false},
		{"a misspelled token kind fails", map[string]any{"type": "gcp", "gcpToken": "id"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Connector{}
			err := c.Start(context.Background(), types.ConnectorConfig{
				Name:     "api",
				Settings: types.Settings{"baseURL": "https://example.invalid", "auth": tc.auth},
			})
			if tc.wantErr && err == nil {
				t.Error("Start succeeded, want an error")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Start: %v", err)
			}
		})
	}
}
