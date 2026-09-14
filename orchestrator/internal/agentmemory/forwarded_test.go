package agentmemory

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

func requestForwarding(value string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if value != "" {
		r.Header.Set(forwardedContextHeader, value)
	}
	return r
}

func TestForwardedContextDecodesWhatTheRuntimeSent(t *testing.T) {
	// What core.EncodeMemoryContext produces: unpadded base64url of compact JSON.
	encoded := base64.RawURLEncoding.EncodeToString([]byte(`{"key":"s3cret","tenant":"acme"}`))
	got, err := forwardedContext(requestForwarding(encoded))
	if err != nil {
		t.Fatalf("forwardedContext: %v", err)
	}
	if got["key"] != "s3cret" || got["tenant"] != "acme" {
		t.Errorf("decoded %v, want the forwarded pair", got)
	}
}

// No header is the ordinary case, not a failure, and it reads as empty rather
// than nil so nothing downstream has to tell those apart.
func TestNoForwardedContextIsEmptyAndNotAnError(t *testing.T) {
	got, err := forwardedContext(requestForwarding(""))
	if err != nil {
		t.Fatalf("forwardedContext: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("got %v, want an empty map", got)
	}
}

// A header that arrived and cannot be read is refused. Our own runtime writes it,
// so a value that does not decode means a caller meant to forward something and
// we did not get it — and serving the call as if nobody had asked is the silent
// wrong answer for the kind of value worth forwarding.
func TestUnreadableForwardedContextIsRefused(t *testing.T) {
	cases := map[string]string{
		"not base64":    "!!!not base64!!!",
		"not an object": base64.RawURLEncoding.EncodeToString([]byte(`["a","list"]`)),
		"null":          base64.RawURLEncoding.EncodeToString([]byte(`null`)),
		// {"k":null} decodes into map[string]string as {"k":""} without complaint, so
		// a key whose value went missing would arrive as an empty one.
		"a null value": base64.RawURLEncoding.EncodeToString([]byte(`{"k":null}`)),
		"values are not strings": base64.RawURLEncoding.EncodeToString(
			[]byte(`{"k":{"nested":"object"}}`)),
		"oversized": base64.RawURLEncoding.EncodeToString(
			[]byte(`{"k":"` + strings.Repeat("x", forwardedContextLimit) + `"}`)),
	}
	for name, value := range cases {
		got, err := forwardedContext(requestForwarding(value))
		if !errors.Is(err, ErrInvalidForwardedContext) {
			t.Errorf("%s: err = %v, want ErrInvalidForwardedContext", name, err)
		}
		if got != nil {
			t.Errorf("%s: returned %v alongside the error", name, got)
		}
	}
}

// The forwarded map must not be settable from a request body, where a caller
// could name a context it was never handed. Query carries it as json:"-".
func TestSearchQueryDoesNotTakeForwardedContextFromTheBody(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(`{"text":"anything","forwarded":{"key":"injected"}}`))
	var q Query
	if err := httpx.DecodeJSON(httptest.NewRecorder(), r, &q); err == nil {
		t.Fatal("want the unknown body field refused, got none")
	}
	if q.Forwarded != nil {
		t.Errorf("Forwarded = %v, want nothing taken from the body", q.Forwarded)
	}
}
