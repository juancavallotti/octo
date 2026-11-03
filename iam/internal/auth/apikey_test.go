package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"github.com/juancavallotti/octo/iam/internal/apikey"
	"github.com/juancavallotti/octo/iam/internal/user"
)

const testKey = "octo_testkeytestkeytestkeytestkeytestkeytest"

// keyFor registers testKey as belonging to whoever signed in as email, carrying
// roles, and returns that person's sign-in.
func (h *harness) keyFor(t *testing.T, subject, email string, roles ...user.Role) authResponse {
	t.Helper()
	owner := h.signIn(t, subject, email)
	h.keys[testKey] = apikey.APIKey{ID: "key-1", UserID: owner.User.ID, Roles: roles}
	return owner
}

// exchangedRoles presents testKey and returns the roles of the token it buys,
// failing unless the exchange answers 200.
func (h *harness) exchangedRoles(t *testing.T) []user.Role {
	t.Helper()
	rec := h.post(t, "Bearer "+testKey)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var private platformClaims
	if _, err := h.signing.Verify(context.Background(), got.Token, &private); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !slices.Equal(got.User.Roles, private.Roles) {
		t.Errorf("the response's user holds %v, the token %v; they must agree", got.User.Roles, private.Roles)
	}
	return private.Roles
}

func TestAnAPIKeyIsExchangedForItsOwnersToken(t *testing.T) {
	h := newHarness(t)
	owner := h.keyFor(t, "provider|abc123", "owner@example.com", user.RoleDeveloper)

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
	// The owner is the first user and so the admin, but the token carries only
	// what the key was minted with.
	if !slices.Equal(private.Roles, []user.Role{user.RoleDeveloper}) {
		t.Errorf("roles = %v, want the key's [%s]", private.Roles, user.RoleDeveloper)
	}
	if private.APIKey != "key-1" {
		t.Errorf("apiKey = %q, want the key it was exchanged for", private.APIKey)
	}
}

// A key minted while its owner held a role stops carrying it once they do not.
func TestAKeyShrinksWithItsOwner(t *testing.T) {
	h := newHarness(t)
	h.signIn(t, "provider|admin", "admin@example.com")
	ctx := context.Background()
	if _, err := h.users.Create(ctx, "op@example.com", "", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	owner := h.keyFor(t, "provider|op", "op@example.com", user.RoleDeveloper, user.RoleOperator)
	if err := h.users.Grant(ctx, owner.User.ID, user.RoleOperator, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if got := h.exchangedRoles(t); !slices.Equal(got, []user.Role{user.RoleDeveloper, user.RoleOperator}) {
		t.Fatalf("as an operator, roles = %v, want both the key carries", got)
	}

	// Demoted to developer: operator falls off the key, developer stays.
	if err := h.users.Revoke(ctx, owner.User.ID, user.RoleOperator); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := h.users.Grant(ctx, owner.User.ID, user.RoleDeveloper, nil); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if got := h.exchangedRoles(t); !slices.Equal(got, []user.Role{user.RoleDeveloper}) {
		t.Fatalf("as a developer, roles = %v, want [%s]", got, user.RoleDeveloper)
	}

	// Left with nothing the key carries: refused, and told why.
	if err := h.users.Revoke(ctx, owner.User.ID, user.RoleDeveloper); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	rec := h.post(t, "Bearer "+testKey)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "no longer held") {
		t.Fatalf("POST /auth = %d (%s), want 403 saying the roles are no longer held", rec.Code, rec.Body)
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
	h.keyFor(t, "provider|abc123", "owner@example.com", user.RoleDeveloper)

	rec := h.post(t, "Bearer "+testKey)
	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if rec := h.postRefresh(t, "Bearer "+got.Token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /auth/refresh = %d, want 401", rec.Code)
	}
}

// A machine token renews itself without asking after any key, so one lent from a
// key's token would keep the key's reach alive after the key is revoked.
func TestATokenFromAnAPIKeyCannotMintAMachineToken(t *testing.T) {
	h := newHarness(t)
	h.keyFor(t, "provider|abc123", "owner@example.com", user.RoleDeveloper)

	rec := h.post(t, "Bearer "+testKey)
	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if rec := h.postMachine(t, "Bearer "+got.Token, "billing"); rec.Code != http.StatusForbidden {
		t.Fatalf("POST /auth/machine = %d (%s), want 403", rec.Code, rec.Body.String())
	}
}
