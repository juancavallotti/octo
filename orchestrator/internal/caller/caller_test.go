package caller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// seen records what the wrapped handler found on the context.
func seen(t *testing.T, header string) string {
	t.Helper()
	var got string
	h := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = Token(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/deployments", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// RFC 6750 says the scheme is case-insensitive, and clients differ.
func TestAnyCasingOfBearerIsRead(t *testing.T) {
	for _, header := range []string{"Bearer a.b.c", "bearer a.b.c", "BEARER a.b.c"} {
		if got := seen(t, header); got != "a.b.c" {
			t.Errorf("Token() = %q for %q, want the token", got, header)
		}
	}
}

// Everything that is not a bearer is nothing at all. There is no partial reading
// of a credential here: the consumer spends it, and a malformed one is refused at
// the far end anyway.
func TestAnythingElseYieldsNoToken(t *testing.T) {
	for name, header := range map[string]string{
		"absent":       "",
		"basic":        "Basic dXNlcjpwYXNz",
		"bare token":   "a.b.c",
		"scheme alone": "Bearer",
		"empty bearer": "Bearer ",
	} {
		t.Run(name, func(t *testing.T) {
			if got := seen(t, header); got != "" {
				t.Errorf("Token() = %q, want none", got)
			}
		})
	}
}

// A context nothing put a token on answers "none" rather than panicking, because
// every consumer already has to handle an install that sends no credential.
func TestTokenOnABareContext(t *testing.T) {
	if got := Token(context.Background()); got != "" {
		t.Errorf("Token() = %q on a bare context, want none", got)
	}
}

func TestWithCarriesATokenSetDirectly(t *testing.T) {
	ctx := With(context.Background(), "a.b.c")
	if got := Token(ctx); got != "a.b.c" {
		t.Errorf("Token() = %q, want the token set on the context", got)
	}
}
