package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeKV is an in-memory, versioned KV with the standalone store's
// optimistic-concurrency semantics. The httpclient tests can't import the
// services module to get a real one (services imports core, which this module
// also imports), so the token-persistence tests use this instead.
type fakeKV struct {
	mu sync.Mutex
	ns map[string]map[string]core.Entry
}

func newFakeKV() *fakeKV { return &fakeKV{ns: make(map[string]map[string]core.Entry)} }

func (s *fakeKV) Get(_ context.Context, namespace, key string) (core.Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.ns[namespace][key]
	if !ok {
		return core.Entry{}, false, nil
	}
	return core.Entry{Value: append([]byte(nil), e.Value...), Version: e.Version}, true, nil
}

func (s *fakeKV) Set(_ context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.ns[namespace]
	current := keys[key].Version
	if expectedVersion != current {
		return 0, core.ErrVersionConflict
	}
	if keys == nil {
		keys = make(map[string]core.Entry)
		s.ns[namespace] = keys
	}
	next := current + 1
	keys[key] = core.Entry{Value: append([]byte(nil), value...), Version: next}
	return next, nil
}

func (s *fakeKV) Delete(_ context.Context, namespace, key string, expectedVersion int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.ns[namespace]
	e, ok := keys[key]
	if !ok {
		return nil
	}
	if expectedVersion != 0 && expectedVersion != e.Version {
		return core.ErrVersionConflict
	}
	delete(keys, key)
	return nil
}

func (s *fakeKV) count(namespace string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ns[namespace])
}

type fakeServices struct{ kv *fakeKV }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) LeaderElection() core.LeaderElection { return core.NoopLeaderElection() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) KV() core.KV { return f.kv }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Secrets() core.SecretStore { return core.NewSecretStore(f.kv) }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Queues() core.Queues { return core.NoopQueues() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Topics() core.Topics { return core.NoopTopics() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

//nolint:ireturn // implements core.RuntimeServices
func (f fakeServices) Traces() core.TracePublisher { return core.NoopTracer() }

func (f fakeServices) Close() error { return nil }

// oauth2Settings builds an http-client settings map configured for the
// client-credentials grant against tokenURL, with apiURL as the base.
func oauth2Settings(apiURL, tokenURL string) types.Settings {
	return types.Settings{
		"baseURL": apiURL,
		"auth": map[string]any{
			"type":         "oauth2",
			"tokenURL":     tokenURL,
			"clientID":     "client-id",
			"clientSecret": "client-secret",
			"scopes":       []any{"read", "write"},
		},
	}
}

func TestOAuth2ClientCredentials(t *testing.T) {
	var tokenCalls atomic.Int64
	var gotGrant, gotScope string
	var gotClientID string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		_ = r.ParseForm()
		gotGrant = r.FormValue("grant_type")
		gotScope = r.FormValue("scope")
		gotClientID, _, _ = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`)
	}))
	defer tokenSrv.Close()

	var gotAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer apiSrv.Close()

	c := startConnector(t, oauth2Settings(apiSrv.URL, tokenSrv.URL))

	get(t, c, "/first")
	if gotAuth != "Bearer tok-1" {
		t.Errorf("Authorization = %q, want Bearer tok-1", gotAuth)
	}
	if gotGrant != "client_credentials" {
		t.Errorf("grant_type = %q, want client_credentials", gotGrant)
	}
	if gotScope != "read write" {
		t.Errorf("scope = %q, want %q", gotScope, "read write")
	}
	if gotClientID != "client-id" {
		t.Errorf("token request client id = %q, want client-id", gotClientID)
	}

	// A second request reuses the cached token rather than minting another.
	get(t, c, "/second")
	if n := tokenCalls.Load(); n != 1 {
		t.Errorf("token endpoint hit %d times, want 1 (token should be cached)", n)
	}
}

func TestOAuth2RefreshesWithRefreshToken(t *testing.T) {
	var mu sync.Mutex
	var grants []string
	var gotRefreshToken string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		grants = append(grants, r.FormValue("grant_type"))
		n := len(grants)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First token expires within the refresh skew, so the next request refreshes.
			_, _ = io.WriteString(w, `{"access_token":"tok-1","refresh_token":"refr-1","expires_in":1}`)
			return
		}
		gotRefreshToken = r.FormValue("refresh_token")
		_, _ = io.WriteString(w, `{"access_token":"tok-2","expires_in":3600}`)
	}))
	defer tokenSrv.Close()

	var mu2 sync.Mutex
	var auths []string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu2.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu2.Unlock()
		_, _ = io.WriteString(w, `{}`)
	}))
	defer apiSrv.Close()

	c := startConnector(t, oauth2Settings(apiSrv.URL, tokenSrv.URL))
	get(t, c, "/a")
	get(t, c, "/b")

	mu.Lock()
	gotGrants := append([]string(nil), grants...)
	mu.Unlock()
	if len(gotGrants) != 2 || gotGrants[0] != "client_credentials" || gotGrants[1] != "refresh_token" {
		t.Fatalf("grants = %v, want [client_credentials refresh_token]", gotGrants)
	}
	if gotRefreshToken != "refr-1" {
		t.Errorf("refresh_token sent = %q, want refr-1", gotRefreshToken)
	}
	mu2.Lock()
	defer mu2.Unlock()
	if len(auths) != 2 || auths[0] != "Bearer tok-1" || auths[1] != "Bearer tok-2" {
		t.Errorf("request auth headers = %v, want [Bearer tok-1, Bearer tok-2]", auths)
	}
}

func TestOAuth2FallsBackWhenRefreshFails(t *testing.T) {
	var mu sync.Mutex
	var grants []string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		grant := r.FormValue("grant_type")
		mu.Lock()
		grants = append(grants, grant)
		mu.Unlock()
		if grant == "refresh_token" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fresh","refresh_token":"refr","expires_in":1}`)
	}))
	defer tokenSrv.Close()

	var lastAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer apiSrv.Close()

	c := startConnector(t, oauth2Settings(apiSrv.URL, tokenSrv.URL))
	get(t, c, "/a") // mints via client_credentials, token expires within skew
	get(t, c, "/b") // refresh fails, falls back to client_credentials

	if lastAuth != "Bearer fresh" {
		t.Errorf("auth after refresh failure = %q, want Bearer fresh", lastAuth)
	}
	mu.Lock()
	defer mu.Unlock()
	// client_credentials, then a failed refresh_token, then client_credentials again.
	if len(grants) != 3 || grants[1] != "refresh_token" || grants[2] != "client_credentials" {
		t.Errorf("grants = %v, want the refresh to fall back to client_credentials", grants)
	}
}

func TestOAuth2PersistsAndAdoptsToken(t *testing.T) {
	var tokenCalls atomic.Int64
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"shared","expires_in":3600}`)
	}))
	defer tokenSrv.Close()
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer apiSrv.Close()

	svc := fakeServices{kv: newFakeKV()}
	ctx := core.ContextWithRuntimeServices(context.Background(), svc)
	cfg := types.ConnectorConfig{Name: "api", Type: "http-client", Settings: oauth2Settings(apiSrv.URL, tokenSrv.URL)}

	first := &Connector{}
	if err := first.Start(ctx, cfg); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer func() { _ = first.Stop(ctx) }()
	get(t, first, "/x")
	if n := tokenCalls.Load(); n != 1 {
		t.Fatalf("token endpoint hit %d times after first request, want 1", n)
	}
	if got := svc.kv.count(core.NamespaceSystemSecrets); got != 1 {
		t.Errorf("persisted secret entries = %d, want 1 (in the system secret namespace)", got)
	}

	// A fresh connector sharing the store adopts the persisted token instead of
	// minting a new one.
	second := &Connector{}
	if err := second.Start(ctx, cfg); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	defer func() { _ = second.Stop(ctx) }()
	get(t, second, "/y")
	if n := tokenCalls.Load(); n != 1 {
		t.Errorf("token endpoint hit %d times, want 1 (second connector should adopt the stored token)", n)
	}
}

func TestOAuth2TokenEndpointErrorSurfaces(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer tokenSrv.Close()
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	defer apiSrv.Close()

	c := startConnector(t, oauth2Settings(apiSrv.URL, tokenSrv.URL))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Error("expected an error when the token endpoint fails")
	}
}

func TestOAuth2StartValidation(t *testing.T) {
	tests := []struct {
		name string
		auth map[string]any
	}{
		{name: "missing tokenURL", auth: map[string]any{"type": "oauth2", "clientID": "i", "clientSecret": "s"}},
		//nolint:gosec // G101: test fixtures, not real credentials
		{name: "missing clientID", auth: map[string]any{"type": "oauth2", "tokenURL": "https://x", "clientSecret": "s"}},
		//nolint:gosec // G101: test fixtures, not real credentials
		{name: "missing clientSecret", auth: map[string]any{"type": "oauth2", "tokenURL": "https://x", "clientID": "i"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Connector{}
			cfg := types.ConnectorConfig{Name: "api", Settings: types.Settings{"baseURL": "https://api", "auth": tt.auth}}
			if err := c.Start(context.Background(), cfg); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
