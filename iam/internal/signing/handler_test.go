package signing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestHandler(t *testing.T) (*http.ServeMux, *Service) {
	t.Helper()
	svc, _ := newTestService(t, Config{Issuer: "https://iam.example"})
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	return mux, svc
}

func get(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// A verifier follows jwks_uri from this document, so an issuer that disagrees
// with the tokens' `iss`, or a URI that is not this service's, breaks every
// caller at once.
func TestDiscoveryDocumentPointsAtOurOwnKeys(t *testing.T) {
	mux, svc := newTestHandler(t)

	rec := get(t, mux, discoveryPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", discoveryPath, rec.Code)
	}

	var doc discoveryDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if doc.Issuer != svc.Issuer() {
		t.Errorf("issuer = %q, want %q", doc.Issuer, svc.Issuer())
	}
	if want := svc.Issuer() + jwksPath; doc.JWKSURI != want {
		t.Errorf("jwks_uri = %q, want %q", doc.JWKSURI, want)
	}
	if len(doc.IDTokenSigningAlgValuesSupported) != 1 ||
		doc.IDTokenSigningAlgValuesSupported[0] != signingAlgorithm {
		t.Errorf("algs = %v, want [%s]", doc.IDTokenSigningAlgValuesSupported, signingAlgorithm)
	}
	// The claims a caller will authorize on have to be advertised, or the document
	// describes a token other than the one this service mints.
	for _, want := range []string{"sub", "email", "roles"} {
		if !contains(doc.ClaimsSupported, want) {
			t.Errorf("claims_supported = %v, want it to include %q", doc.ClaimsSupported, want)
		}
	}
}

// This service authenticates nobody and runs no authorization flow, so it must
// not advertise the endpoints of one.
func TestDiscoveryDocumentAdvertisesNoAuthorizationFlow(t *testing.T) {
	mux, _ := newTestHandler(t)

	var raw map[string]any
	if err := json.Unmarshal(get(t, mux, discoveryPath).Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, absent := range []string{"authorization_endpoint", "token_endpoint", "userinfo_endpoint"} {
		if _, present := raw[absent]; present {
			t.Errorf("the document advertises %q, which this service does not serve", absent)
		}
	}
}

func TestJWKSIsServedAndCacheable(t *testing.T) {
	mux, _ := newTestHandler(t)

	rec := get(t, mux, jwksPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", jwksPath, rec.Code)
	}

	var set struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &set); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("the set holds %d keys, want 1", len(set.Keys))
	}
	// Publishing the private half would hand anyone who can reach this endpoint
	// the ability to mint platform tokens, so it is asserted on the wire bytes
	// and not only on the Go value.
	for _, field := range []string{"d", "p", "q", "dp", "dq", "qi"} {
		if _, present := set.Keys[0][field]; present {
			t.Errorf("the published key carries the private field %q: %s", field, rec.Body.String())
		}
	}
	if got := set.Keys[0]["kty"]; got != "EC" {
		t.Errorf("kty = %v, want EC", got)
	}

	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age=") {
		t.Errorf("Cache-Control = %q, want a max-age so callers cache the keys", cc)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
