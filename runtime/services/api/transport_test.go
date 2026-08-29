package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withURL builds the module's configuration around one base URL, so the
// validation under test runs the way it does at startup.
func withURL(t *testing.T, url string, env map[string]string) (Config, error) {
	t.Helper()
	t.Setenv(envURL, url)
	for k, v := range env {
		t.Setenv(k, v)
	}
	return loadConfig()
}

// A credential over plaintext HTTP is a credential on the wire in the clear, and
// no deployment wants that badly enough to be given it silently.
func TestCredentialsRefusedOverPlaintextHTTP(t *testing.T) {
	cases := []struct{ name, key, value string }{
		{"bearer token", envToken, "s3cret"},
		{"token file", envTokenFile, "/run/secrets/token"},
		{"an api key header", envHeaders, "X-Api-Key: abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := withURL(t, "http://platform.example.internal", map[string]string{tc.key: tc.value})
			if err == nil {
				t.Fatal("LoadConfig accepted a credential over plaintext http")
			}
			if !strings.Contains(err.Error(), "plaintext http") {
				t.Fatalf("err = %v, want it to name the reason", err)
			}
		})
	}
}

// The sidecar deployment IS plaintext to loopback, and those bytes never leave
// the pod. Requiring TLS there would mean issuing a certificate for 127.0.0.1.
func TestCredentialsAllowedOverLoopback(t *testing.T) {
	for _, host := range []string{"http://127.0.0.1:8080", "http://localhost:8080", "http://[::1]:8080"} {
		if _, err := withURL(t, host, map[string]string{envToken: "s3cret"}); err != nil {
			t.Errorf("LoadConfig(%s) = %v, want the sidecar shape to work", host, err)
		}
	}
}

// A plaintext endpoint with no credential is somebody's private network. The
// contract carries no secrets of its own, and refusing it would only push people
// into setting a token they do not need.
func TestPlaintextWithoutCredentialsIsFine(t *testing.T) {
	if _, err := withURL(t, "http://platform.example.internal", nil); err != nil {
		t.Fatalf("LoadConfig = %v, want plaintext without credentials to be allowed", err)
	}
}

// HTTPS is always fine.
func TestHTTPSWithCredentials(t *testing.T) {
	if _, err := withURL(t, "https://platform.example.internal", map[string]string{envToken: "s"}); err != nil {
		t.Fatalf("LoadConfig = %v", err)
	}
}

// A URL carrying credentials would be written to the log of every runtime that
// used it, because the URL is logged at startup and on every discovery retry.
func TestURLWithCredentialsIsRefused(t *testing.T) {
	_, err := withURL(t, "https://user:pass@platform.example.internal", nil)
	if err == nil {
		t.Fatal("LoadConfig accepted credentials embedded in the URL")
	}
	// Rejected rather than redacted: somebody who wrote them there believes that
	// is how this authenticates, and dropping them silently would leave them with
	// a runtime that cannot log in and no reason why.
	if !strings.Contains(err.Error(), envToken) {
		t.Fatalf("err = %v, want it to name the variables to use instead", err)
	}
}

func TestMalformedURLsAreRefused(t *testing.T) {
	cases := []struct{ name, url, want string }{
		{"a scheme this cannot speak", "ftp://platform.example", "http or https"},
		{"no host", "https://", "no host"},
		{"a query that would be dropped", "https://platform.example?tenant=acme", "query or fragment"},
		{"a fragment that would be dropped", "https://platform.example#frag", "query or fragment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := withURL(t, tc.url, nil)
			if err == nil {
				t.Fatalf("LoadConfig(%s) err = nil, want a refusal", tc.url)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// redirectClient builds a client with the given credentials, for the redirect
// policy tests.
func redirectClient(t *testing.T, base string, cfg Config) *client {
	t.Helper()
	cfg.BaseURL = base
	cfg.Timeout, cfg.LongTimeout = defaultTimeout, defaultLongTimeout
	c, err := newClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)
	return c
}

// A credential must never land on a plaintext hop that is not loopback, whatever
// the request started as.
//
// The loopback case is the one an https-only rule missed: a sidecar is ALLOWED to
// hold credentials over plaintext, so a sidecar redirecting to a plaintext host
// on the network would have handed them over. And net/http drops Authorization
// across hosts but never touches custom headers, so an API key from
// OCTO_PLATFORM_API_HEADERS followed a redirect anywhere at all.
func TestRedirectToPlaintextIsRefused(t *testing.T) {
	cases := []struct{ name, from, to string }{
		{"https to plaintext", "https://a.example/x", "http://b.example/x"},
		{"loopback to plaintext on the network", "http://127.0.0.1:8080/x", "http://b.example/x"},
		{"plaintext to plaintext elsewhere", "http://a.example/x", "http://b.example/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := redirectClient(t, tc.from, Config{Headers: map[string]string{"X-Api-Key": "k"}})
			err := c.checkRedirect(request(t, tc.to), []*http.Request{request(t, tc.from)})
			if err == nil {
				t.Fatal("the redirect was allowed")
			}
			if !strings.Contains(err.Error(), "plaintext") {
				t.Fatalf("err = %v, want it to name the reason", err)
			}
		})
	}
}

// A credential belongs to the host it was configured for. Crossing hosts strips
// it rather than refusing, which is what net/http already does for Authorization
// and keeps a redirect to a canonical hostname working.
func TestCrossHostRedirectStripsCredentials(t *testing.T) {
	c := redirectClient(t, "https://a.example", Config{
		Token:   "s3cret",
		Headers: map[string]string{"X-Api-Key": "k", "X-Tenant": "acme"},
	})
	to := request(t, "https://b.example/x")
	to.Header.Set("Authorization", "Bearer s3cret")
	to.Header.Set("X-Api-Key", "k")
	to.Header.Set("X-Tenant", "acme")
	to.Header.Set("X-Octo-Instance", "runtime-1")

	if err := c.checkRedirect(to, []*http.Request{request(t, "https://a.example/x")}); err != nil {
		t.Fatalf("checkRedirect = %v, want the redirect allowed with the credentials removed", err)
	}
	for _, name := range []string{"Authorization", "X-Api-Key", "X-Tenant"} {
		if got := to.Header.Get(name); got != "" {
			t.Errorf("%s survived a cross-host redirect as %q", name, got)
		}
	}
	// The module's own headers are not credentials and may follow.
	if to.Header.Get("X-Octo-Instance") == "" {
		t.Error("X-Octo-Instance was stripped; it identifies the caller, it is not a secret")
	}
}

// Same host, same credentials: nothing to strip.
func TestSameHostRedirectKeepsCredentials(t *testing.T) {
	c := redirectClient(t, "https://a.example", Config{Headers: map[string]string{"X-Api-Key": "k"}})
	to := request(t, "https://a.example/moved")
	to.Header.Set("X-Api-Key", "k")

	if err := c.checkRedirect(to, []*http.Request{request(t, "https://a.example/x")}); err != nil {
		t.Fatalf("checkRedirect = %v", err)
	}
	if to.Header.Get("X-Api-Key") == "" {
		t.Error("a same-host redirect stripped the credential it was configured with")
	}
}

// With nothing to protect, redirects are left alone — including the plaintext
// ones, which are somebody's private network.
func TestRedirectsWithoutCredentialsAreLeftAlone(t *testing.T) {
	c := redirectClient(t, "http://a.example", Config{})
	if err := c.checkRedirect(request(t, "http://b.example/x"),
		[]*http.Request{request(t, "http://a.example/x")}); err != nil {
		t.Fatalf("checkRedirect = %v, want it allowed when there is nothing to leak", err)
	}
}

// And the policy is actually installed on the client, not merely defined.
func TestClientInstallsTheRedirectPolicy(t *testing.T) {
	downgraded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://platform.example.internal/v1/discovery", http.StatusFound)
	}))
	t.Cleanup(downgraded.Close)

	c := redirectClient(t, downgraded.URL, Config{Headers: map[string]string{"X-Api-Key": "k"}})
	err := c.json(t.Context(), routeDiscovery, c.url(routeDiscovery), nil, nil, defaultTimeout)
	if err == nil {
		t.Fatal("the client followed a redirect that would have leaked its credentials")
	}
}

// request builds a request for one URL.
func request(t *testing.T, raw string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}
