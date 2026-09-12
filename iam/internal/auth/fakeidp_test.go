package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIDP is a standards-compliant OIDC issuer, small enough to run inside a
// test: a discovery document and a key set, over an httptest server. The
// verifier under test reaches it exactly as it would reach a real provider —
// discovery, then a signature check against the published keys — so what these
// tests exercise is the real go-oidc path and not a stub of it.
//
// It signs RS256 because that is the only algorithm go-oidc accepts unless it is
// told otherwise, and the verifier deliberately does not tell it otherwise: an
// install should not be able to widen what its provider may sign with by
// accident.
type fakeIDP struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	kid      string
	clientID string

	// discoveries counts requests to the discovery document, so a test can assert
	// that a warm verifier stops asking.
	mu          sync.Mutex
	discoveries int

	// userinfo is what GET /userinfo answers with. Nil means the provider has no
	// profile to give, which is how a provider without the endpoint behaves as far
	// as the verifier can tell.
	userinfo map[string]any
}

func (f *fakeIDP) discoveryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.discoveries
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate idp key: %v", err)
	}

	idp := &fakeIDP{key: key, kid: "test-key", clientID: "octo-platform"}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		idp.mu.Lock()
		idp.discoveries++
		idp.mu.Unlock()
		writeJSON(w, map[string]any{
			"issuer":                                idp.Issuer(),
			"authorization_endpoint":                idp.Issuer() + "/authorize",
			"token_endpoint":                        idp.Issuer() + "/token",
			"jwks_uri":                              idp.Issuer() + "/jwks",
			"userinfo_endpoint":                     idp.Issuer() + "/userinfo",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key: key.Public(), KeyID: idp.kid, Algorithm: string(jose.RS256), Use: "sig",
		}}})
	})

	mux.HandleFunc("GET /userinfo", func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		claims := idp.userinfo
		idp.mu.Unlock()
		if claims == nil {
			http.Error(w, "no userinfo here", http.StatusNotFound)
			return
		}
		// A real userinfo endpoint answers to a credential and not to anybody who
		// asks. Checked here so the test proves the verifier actually presents the
		// caller's token, rather than passing whether or not it sent one.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "a bearer token is required", http.StatusUnauthorized)
			return
		}
		writeJSON(w, claims)
	})

	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeIDP) Issuer() string {
	if f.server == nil {
		// Read while the mux is still being built, before the server exists. The
		// handlers above only call it once a request arrives, by which time it does.
		return ""
	}
	return f.server.URL
}

// idToken mints a token the way the provider would. The options let a case bend
// exactly one thing about it and leave the rest correct, which is what makes a
// rejection attributable.
type tokenOptions struct {
	subject  string
	email    string
	name     string
	audience string
	issuer   string
	expiry   time.Time
	// signWith, when set, signs with a key the provider does not publish.
	signWith *rsa.PrivateKey
}

func (f *fakeIDP) idToken(t *testing.T, opts tokenOptions) string {
	t.Helper()

	if opts.audience == "" {
		opts.audience = f.clientID
	}
	if opts.issuer == "" {
		opts.issuer = f.Issuer()
	}
	if opts.expiry.IsZero() {
		opts.expiry = time.Now().Add(time.Hour)
	}
	key := f.key
	if opts.signWith != nil {
		key = opts.signWith
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), f.kid),
	)
	if err != nil {
		t.Fatalf("new idp signer: %v", err)
	}

	now := time.Now()
	registered := jwt.Claims{
		Issuer:    opts.issuer,
		Subject:   opts.subject,
		Audience:  jwt.Audience{opts.audience},
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
		NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		Expiry:    jwt.NewNumericDate(opts.expiry),
	}
	profile := map[string]any{"email": opts.email, "name": opts.name}

	token, err := jwt.Signed(signer).Claims(registered).Claims(profile).Serialize()
	if err != nil {
		t.Fatalf("sign id token: %v", err)
	}
	return token
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
