package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/juancavallotti/octo/iam/internal/apikey"
	"github.com/juancavallotti/octo/iam/internal/user"
)

const testKey = "octo_testkeytestkeytestkeytestkeytestkeytest"

// keyFor registers testKey as belonging to whoever signed in as email, and
// returns that person's sign-in.
func (h *harness) keyFor(t *testing.T, subject, email string) authResponse {
	t.Helper()
	owner := h.signIn(t, subject, email)
	h.keys[testKey] = apikey.APIKey{ID: "key-1", UserID: owner.User.ID}
	return owner
}

func TestAnAPIKeyIsExchangedForItsOwnersToken(t *testing.T) {
	h := newHarness(t)
	owner := h.keyFor(t, "provider|abc123", "owner@example.com")

	rec := h.post(t, "Bearer "+testKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.User.ID != owner.User.ID {
		t.Fatalf("the token speaks for %q, want the key's owner %q", got.User.ID, owner.User.ID)
	}

	set, err := h.signing.JWKS(context.Background())
	if err != nil {
		t.Fatalf("JWKS: %v", err)
	}
	parsed, err := josejwt.ParseSigned(got.Token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var private platformClaims
	var registered josejwt.Claims
	if err := parsed.Claims(set.Key(parsed.Headers[0].KeyID)[0].Key, &registered, &private); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if registered.Subject != owner.User.ID {
		t.Errorf("sub = %q, want %q", registered.Subject, owner.User.ID)
	}
	// The owner is the first user and so the admin: the key carries what they hold.
	if len(private.Roles) != 1 || private.Roles[0] != user.RoleAdmin {
		t.Errorf("roles = %v, want the owner's [%s]", private.Roles, user.RoleAdmin)
	}
	if private.APIKey != "key-1" {
		t.Errorf("apiKey = %q, want the key it was exchanged for", private.APIKey)
	}
}

func TestAnUnknownAPIKeyIsRefused(t *testing.T) {
	h := newHarness(t)
	rec := h.post(t, "Bearer "+testKey)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth = %d, want 401", rec.Code)
	}
}

// A revoked key must stop working within one token lifetime, which it could not
// if the tokens it was exchanged for renewed themselves without asking after it.
func TestATokenFromAnAPIKeyIsNotRefreshed(t *testing.T) {
	h := newHarness(t)
	h.keyFor(t, "provider|abc123", "owner@example.com")

	rec := h.post(t, "Bearer "+testKey)
	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if rec := h.postRefresh(t, "Bearer "+got.Token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth/refresh = %d, want 401", rec.Code)
	}
}
