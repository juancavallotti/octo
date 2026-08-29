package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

// net/http keeps the Authorization header across a same-host redirect, so an
// https endpoint answering 302 to its own http:// URL would put the token on the
// wire in the clear with nothing noticing.
func TestRedirectDowngradeIsRefused(t *testing.T) {
	from := &http.Request{URL: mustURL(t, "https://platform.example/v1/kv/user/entry")}
	to := &http.Request{URL: mustURL(t, "http://platform.example/v1/kv/user/entry")}

	err := refuseInsecureRedirect(to, []*http.Request{from})
	if err == nil {
		t.Fatal("an https to http redirect was allowed")
	}
	if !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("err = %v, want it to name the downgrade", err)
	}
}

// The redirects that are fine stay fine.
func TestAllowedRedirects(t *testing.T) {
	cases := []struct{ name, from, to string }{
		{"https to https", "https://a.example/x", "https://b.example/x"},
		{"http to http", "http://a.example/x", "http://a.example/y"},
		{"https to loopback", "https://a.example/x", "http://127.0.0.1:8080/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from := &http.Request{URL: mustURL(t, tc.from)}
			to := &http.Request{URL: mustURL(t, tc.to)}
			if err := refuseInsecureRedirect(to, []*http.Request{from}); err != nil {
				t.Fatalf("refuseInsecureRedirect = %v, want it allowed", err)
			}
		})
	}
}

// And the policy is actually installed on the client, not merely defined.
func TestClientInstallsTheRedirectPolicy(t *testing.T) {
	downgraded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to a non-loopback plaintext host: the shape the policy exists for.
		http.Redirect(w, r, "http://platform.example.internal/v1/discovery", http.StatusFound)
	}))
	t.Cleanup(downgraded.Close)

	c, err := newClient(Config{BaseURL: downgraded.URL, Timeout: defaultTimeout, LongTimeout: defaultLongTimeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)

	err = c.json(t.Context(), routeDiscovery, c.url(routeDiscovery), nil, nil, defaultTimeout)
	if err == nil {
		t.Fatal("the client followed a downgrading redirect")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
