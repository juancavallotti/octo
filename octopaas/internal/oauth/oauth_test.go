package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/octopaas/internal/credentials"
)

// provider is a small identity provider with DCR, and an API that names it. It
// checks what a real one would: the PKCE verifier against the challenge, and
// that every token request names the API as its resource.
type provider struct {
	t         *testing.T
	api       *httptest.Server
	idp       *httptest.Server
	challenge string
	resources []string
}

func newProvider(t *testing.T) *provider {

	p := &provider{t: t}
	p.idp = httptest.NewServer(http.HandlerFunc(p.serveIDP))
	p.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me":
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+p.api.URL+`/.well-known/oauth-protected-resource"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/.well-known/oauth-protected-resource":
			_ = json.NewEncoder(w).Encode(map[string]any{"resource": p.api.URL, "authorization_servers": []string{p.idp.URL}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { p.api.Close(); p.idp.Close() })
	return p
}

func (p *provider) serveIDP(w http.ResponseWriter, r *http.Request) {

	switch r.URL.Path {
	case "/.well-known/oauth-authorization-server":
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 p.idp.URL,
			"authorization_endpoint": p.idp.URL + "/authorize",
			"token_endpoint":         p.idp.URL + "/token",
			"registration_endpoint":  p.idp.URL + "/register",
		})
	case "/register":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["scope"] != "openid profile email" {
			p.t.Errorf("octopaas should register for the scopes it signs in with, sent %v", body["scope"])
		}
		if body["token_endpoint_auth_method"] != "none" {
			p.t.Errorf("octopaas should register as a public client, sent %v", body["token_endpoint_auth_method"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-1"})
	case "/authorize":
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" {
			p.t.Errorf("sign-in should use PKCE S256, got %q", q.Get("code_challenge_method"))
		}
		p.challenge = q.Get("code_challenge")
		p.resources = append(p.resources, q.Get("resource"))
		back, _ := url.Parse(q.Get("redirect_uri"))
		back.RawQuery = url.Values{"code": {"code-1"}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
	case "/token":
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		p.resources = append(p.resources, r.Form.Get("resource"))
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"PKCE verification failed"}`))
				return
			}
			_, _ = fmt.Fprint(w, `{"access_token":"at-1","refresh_token":"rt-1","token_type":"Bearer","expires_in":3600}`)
		case "refresh_token":
			_, _ = fmt.Fprint(w, `{"access_token":"at-2","token_type":"Bearer","expires_in":3600}`)
		}
	default:
		http.NotFound(w, r)
	}
}

// The whole sign-in, from nothing but the API's URL: discover, register, send
// the browser through, trade the code. A browser is played by following the
// authorization URL's redirect back to our listener.
func TestSignInFromTheAPIAlone(t *testing.T) {

	p := newProvider(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	md, err := Discover(ctx, http.DefaultClient, p.api.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if md.Issuer != p.idp.URL || md.Resource != p.api.URL {
		t.Fatalf("discovered %+v", md)
	}

	listener, redirect, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	client, err := Register(ctx, http.DefaultClient, md, redirect)
	if err != nil || client.ClientID != "client-1" {
		t.Fatalf("Register: %+v %v", client, err)
	}

	login := &Login{
		HTTP:     http.DefaultClient,
		Metadata: md,
		Client:   client,
		Listener: listener,
		Open: func(u string) error {
			go func() {
				resp, err := http.Get(u)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
			return nil
		},
		Page: func(ok bool, msg string) string { return msg },
	}
	token, err := login.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if token.AccessToken != "at-1" || token.RefreshToken != "rt-1" {
		t.Errorf("token = %+v", token)
	}

	//and later, the session renews itself, still asking for the API.
	session := credentials.Session{Resource: md.Resource, ClientID: client.ClientID, TokenEndpoint: md.TokenEndpoint, AccessToken: "at-1", RefreshToken: "rt-1"}
	renewed, err := Refresh(ctx, http.DefaultClient, session)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if renewed.AccessToken != "at-2" || renewed.RefreshToken != "rt-1" {
		t.Errorf("a provider that does not rotate keeps the old refresh token; got %+v", renewed)
	}

	for i, r := range p.resources {
		if r != p.api.URL {
			t.Errorf("request %d asked for resource %q, want the API (%s)", i, r, p.api.URL)
		}
	}
}

// A provider that cannot register clients is a clear dead end, pointing at the
// alternative.
func TestAProviderWithoutRegistrationIsExplained(t *testing.T) {

	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": idp.URL, "authorization_endpoint": "a", "token_endpoint": "t"})
	}))
	defer idp.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{idp.URL}})
	}))
	defer api.Close()

	_, err := Discover(context.Background(), http.DefaultClient, api.URL)
	if err != ErrNoRegistration {
		t.Fatalf("want ErrNoRegistration, got %v", err)
	}
}

// A provider claiming to be somebody else is not signed in with.
func TestAnIssuerMismatchIsRefused(t *testing.T) {

	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": "https://evil.example.com"})
	}))
	defer idp.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authorization_servers": []string{idp.URL}})
	}))
	defer api.Close()

	_, err := Discover(context.Background(), http.DefaultClient, api.URL)
	if err == nil || !strings.Contains(err.Error(), "not who the API says") {
		t.Fatalf("an issuer mismatch should be refused, got %v", err)
	}
}

// An expired session renews itself and is saved, so the next command starts
// from the new token.
func TestSourceRenewsAndSaves(t *testing.T) {

	p := newProvider(t)
	home := t.TempDir()
	store, _ := credentials.Load(home)
	store.Sessions["local"] = credentials.Session{
		Resource: p.api.URL, ClientID: "client-1", TokenEndpoint: p.idp.URL + "/token",
		AccessToken: "old", RefreshToken: "rt-1", Expiry: time.Now().Add(-time.Minute),
	}
	if err := store.Save(home); err != nil {
		t.Fatal(err)
	}

	src := &Source{HTTP: http.DefaultClient, Home: home, Profile: "local"}
	got, err := src.Bearer(context.Background(), false)
	if err != nil || got != "at-2" {
		t.Fatalf("an expired token should be renewed, got %q %v", got, err)
	}
	back, _ := credentials.Load(home)
	if back.Sessions["local"].AccessToken != "at-2" {
		t.Errorf("the renewed token should be saved, got %+v", back.Sessions["local"])
	}

	signedOut := &Source{HTTP: http.DefaultClient, Home: home, Profile: "prod"}
	if _, err := signedOut.Bearer(context.Background(), false); err != ErrSignedOut {
		t.Errorf("a profile with no session should be ErrSignedOut, got %v", err)
	}
}

func TestResourceMetadataFrom(t *testing.T) {

	got := ResourceMetadataFrom(`Bearer resource_metadata="https://api.example.com/.well-known/oauth-protected-resource"`)
	if got != "https://api.example.com/.well-known/oauth-protected-resource" {
		t.Errorf("got %q", got)
	}
	if ResourceMetadataFrom("Bearer realm=x") != "" {
		t.Error("a challenge without resource_metadata should give nothing")
	}
}
