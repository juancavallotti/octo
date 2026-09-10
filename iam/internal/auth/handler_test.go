package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/juancavallotti/octo/iam/internal/signing"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// The exchange's collaborators are the real ones here — the real user service
// over an in-memory repository, the real signing service over an in-memory keyset
// — so what this covers is the whole path a sign-in takes, minus Postgres. The
// only thing faked is the identity provider, and that is a real OIDC server too;
// see fakeidp_test.go.
type harness struct {
	mux     *http.ServeMux
	idp     *fakeIDP
	signing *signing.Service
	users   *user.Service
}

func newHarness(t *testing.T, grace ...time.Duration) *harness {
	t.Helper()
	idp := newFakeIDP(t)

	users := user.NewService(newMemUsers())
	signer, err := signing.NewService(newMemKeys(), signing.Config{
		Issuer: "https://iam.example", Audience: "octo",
	})
	if err != nil {
		t.Fatalf("signing.NewService: %v", err)
	}
	window := DefaultRefreshGrace
	if len(grace) == 1 {
		window = grace[0]
	}
	svc, err := NewService(NewVerifier(idp.Issuer(), []string{idp.clientID}), users, signer, window)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	return &harness{mux: mux, idp: idp, signing: signer, users: users}
}

func (h *harness) postRefresh(t *testing.T, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

func (h *harness) post(t *testing.T, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth", nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

type authResponse struct {
	Token     string        `json:"token"`
	ExpiresAt time.Time     `json:"expiresAt"`
	User      user.Response `json:"user"`
}

// The whole point, end to end: a provider's token goes in, and what comes out is
// a token any other service can verify from the published keys alone.
func TestExchangeMintsATokenVerifiableFromTheJWKS(t *testing.T) {
	h := newHarness(t)

	rec := h.post(t, "Bearer "+h.idp.idToken(t, tokenOptions{
		subject: "provider|abc123", email: "first@example.com", name: "First Person",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth = %d (%s), want 200", rec.Code, rec.Body.String())
	}

	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Token == "" {
		t.Fatal("no token was returned")
	}

	// Verified the way a downstream service must: fetch the key set, pick the key
	// the header names, check with that alone.
	set, err := h.signing.JWKS(context.Background())
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	parsed, err := josejwt.ParseSigned(got.Token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse minted token: %v", err)
	}
	keys := set.Key(parsed.Headers[0].KeyID)
	if len(keys) != 1 {
		t.Fatalf("the published set holds %d keys for this token's kid, want 1", len(keys))
	}

	var (
		registered josejwt.Claims
		private    struct {
			Email string      `json:"email"`
			Name  string      `json:"name"`
			Roles []user.Role `json:"roles"`
		}
	)
	if err := parsed.Claims(keys[0].Key, &registered, &private); err != nil {
		t.Fatalf("verify minted token: %v", err)
	}

	// The subject is the octo user id, not the provider's. That substitution is
	// the exchange: downstream, a principal is named by an identifier this
	// platform owns.
	if registered.Subject == "provider|abc123" {
		t.Error("the minted token carries the provider's subject")
	}
	if registered.Subject != got.User.ID {
		t.Errorf("sub = %q, want the octo user id %q", registered.Subject, got.User.ID)
	}
	if registered.Issuer != h.signing.Issuer() {
		t.Errorf("iss = %q, want %q", registered.Issuer, h.signing.Issuer())
	}
	if private.Email != "first@example.com" || private.Name != "First Person" {
		t.Errorf("profile claims = %+v, want the provider's", private)
	}
	// First user ever, so they are the admin.
	if len(private.Roles) != 1 || private.Roles[0] != user.RoleAdmin {
		t.Errorf("roles = %v, want [%s]", private.Roles, user.RoleAdmin)
	}
	if got.ExpiresAt.IsZero() {
		t.Error("expiresAt was not reported; a client would have to decode the token")
	}
}

// A grant made between two sign-ins has to reach the next token, or an admin
// granting a role would have no way to make it take effect.
func TestASubsequentExchangeCarriesNewlyGrantedRoles(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	// The first user takes the admin role, so the second starts with none.
	h.post(t, "Bearer "+h.idp.idToken(t, tokenOptions{subject: "admin", email: "admin@example.com"}))

	rec := h.post(t, "Bearer "+h.idp.idToken(t, tokenOptions{
		subject: "provider|second", email: "second@example.com",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var first authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(first.User.Roles) != 0 {
		t.Fatalf("the second user starts with %v, want no roles", first.User.Roles)
	}

	if err := h.users.Grant(ctx, first.User.ID, user.RoleOperator, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}

	rec = h.post(t, "Bearer "+h.idp.idToken(t, tokenOptions{
		subject: "provider|second", email: "second@example.com",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("second POST /auth = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var second authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if second.User.ID != first.User.ID {
		t.Errorf("id changed between sign-ins: %q then %q", first.User.ID, second.User.ID)
	}
	if len(second.User.Roles) != 1 || second.User.Roles[0] != user.RoleOperator {
		t.Errorf("roles = %v, want [%s]", second.User.Roles, user.RoleOperator)
	}
}

func TestExchangeRequiresABearerToken(t *testing.T) {
	h := newHarness(t)

	tests := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"another scheme", "Basic dXNlcjpwYXNz"},
		{"the scheme with nothing after it", "Bearer "},
		{"the word alone", "Bearer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := h.post(t, tt.header)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("POST /auth = %d, want 401", rec.Code)
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("no WWW-Authenticate challenge was sent")
			}
		})
	}
}

// RFC 6750 makes the scheme name case-insensitive and clients disagree about how
// to spell it.
func TestExchangeAcceptsAnyCasingOfBearer(t *testing.T) {
	h := newHarness(t)
	token := h.idp.idToken(t, tokenOptions{subject: "s", email: "a@example.com"})

	for _, scheme := range []string{"Bearer ", "bearer ", "BEARER "} {
		rec := h.post(t, scheme+token)
		if rec.Code != http.StatusOK {
			t.Errorf("POST /auth with %q = %d (%s), want 200",
				scheme, rec.Code, rec.Body.String())
		}
	}
}

func TestExchangeRejectsABadToken(t *testing.T) {
	h := newHarness(t)

	rec := h.post(t, "Bearer not-a-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth = %d, want 401", rec.Code)
	}
	// The reason a token failed is a hint to whoever is guessing at one, so it is
	// logged and not returned.
	if body := rec.Body.String(); strings.Contains(body, "malformed") ||
		strings.Contains(body, "signature") || strings.Contains(body, "audience") {
		t.Errorf("the rejection explains which check failed: %s", body)
	}
}

// The token is a credential and must not be stored by anything on the way back.
func TestExchangeResponseIsNotCacheable(t *testing.T) {
	h := newHarness(t)

	rec := h.post(t, "Bearer "+h.idp.idToken(t, tokenOptions{subject: "s", email: "a@example.com"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

// An install with no identity provider must say so, rather than the route
// disappearing — a 404 leaves a caller unable to tell a misconfigured install
// from a build that never had the feature.
func TestExchangeWithoutAProviderReportsItselfUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(nil).Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/auth", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /auth = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OIDC_ISSUER") {
		t.Errorf("the refusal does not name the missing setting: %s", rec.Body.String())
	}
}

func TestNewServiceRequiresEveryCollaborator(t *testing.T) {
	allUsers := user.NewService(newMemUsers())
	signer, err := signing.NewService(newMemKeys(), signing.Config{
		Issuer: "https://iam.example", Audience: "octo",
	})
	if err != nil {
		t.Fatalf("signing.NewService: %v", err)
	}
	v := NewVerifier("https://idp.example", []string{"octo"})

	tests := []struct {
		name  string
		verif verifier
		store users
		mint  minter
	}{
		{"no verifier", nil, allUsers, signer},
		{"no users", v, nil, signer},
		{"no minter", v, allUsers, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewService(tt.verif, tt.store, tt.mint, 0); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("NewService() error = %v, want ErrNotConfigured", err)
			}
		})
	}
}

// --- refresh ---------------------------------------------------------------

// signIn exchanges a provider token and returns the platform token it minted,
// which is what a refresh arrives holding.
func (h *harness) signIn(t *testing.T, subject, email string) authResponse {
	t.Helper()
	rec := h.post(t, "Bearer "+h.idp.idToken(t, tokenOptions{subject: subject, email: email}))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

func TestRefreshMintsAFreshTokenForTheSameUser(t *testing.T) {
	h := newHarness(t)
	first := h.signIn(t, "provider|abc123", "first@example.com")

	rec := h.postRefresh(t, "Bearer "+first.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/refresh = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var second authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if second.User.ID != first.User.ID {
		t.Errorf("refresh returned user %q, want %q", second.User.ID, first.User.ID)
	}
	if second.Token == "" {
		t.Fatal("no token was returned")
	}
	if !second.ExpiresAt.After(first.ExpiresAt) && !second.ExpiresAt.Equal(first.ExpiresAt) {
		t.Errorf("the refreshed token expires at %v, before the original's %v",
			second.ExpiresAt, first.ExpiresAt)
	}
}

// The whole reason a refresh re-reads the database rather than re-signing what it
// was given: a role taken away must take effect within one token lifetime, not at
// the next sign-in.
func TestRefreshPicksUpARoleChange(t *testing.T) {
	h := newHarness(t)
	first := h.signIn(t, "provider|abc123", "first@example.com")

	// The first user to sign in is made an admin, so this revokes something that
	// is really there. A second admin first, or the last-admin rule refuses.
	second := h.signIn(t, "provider|second", "second@example.com")
	if err := h.users.Grant(context.Background(), second.User.ID, user.RoleAdmin, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := h.users.Revoke(context.Background(), first.User.ID, user.RoleAdmin); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	rec := h.postRefresh(t, "Bearer "+first.Token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/refresh = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var refreshed authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, role := range refreshed.User.Roles {
		if role == user.RoleAdmin {
			t.Fatal("the refreshed token still carries the revoked role")
		}
	}
}

// The window widens expiry and nothing else. A provider's own token, or anything
// else this service did not mint, is not a refresh credential.
func TestRefreshRejectsATokenItDidNotMint(t *testing.T) {
	h := newHarness(t)
	h.signIn(t, "provider|abc123", "first@example.com") // so a keyset exists

	providerToken := h.idp.idToken(t, tokenOptions{subject: "s", email: "a@example.com"})
	if rec := h.postRefresh(t, "Bearer "+providerToken); rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /auth/refresh with a provider token = %d, want 401", rec.Code)
	}
	for _, bearer := range []string{"Bearer not-a-token", "Bearer a.b.c"} {
		if rec := h.postRefresh(t, bearer); rec.Code != http.StatusUnauthorized {
			t.Errorf("POST /auth/refresh with %q = %d, want 401", bearer, rec.Code)
		}
	}
}

func TestRefreshRequiresABearerToken(t *testing.T) {
	h := newHarness(t)
	rec := h.postRefresh(t, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth/refresh = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("no challenge was sent with the 401")
	}
}

// It hands out a credential, so the same no-store rule the exchange follows
// applies here.
func TestRefreshResponseIsNotCacheable(t *testing.T) {
	h := newHarness(t)
	got := h.signIn(t, "provider|abc123", "first@example.com")

	rec := h.postRefresh(t, "Bearer "+got.Token)
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}
