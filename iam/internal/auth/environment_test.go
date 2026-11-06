package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/juancavallotti/octo/iam/internal/apikey"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// machineIn mints a machine token in environment on bearer's authority, failing
// unless iam answers 200, and returns the token with what it carries.
func (h *harness) machineIn(
	t *testing.T, bearer, environment string, access ...Access,
) (string, machinePrivate) {
	t.Helper()
	rec := h.postMachineIn(t, "Bearer "+bearer, "dep-1", environment, access...)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/machine = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var got authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, private := h.claimsOf(t, got.Token)
	return got.Token, private
}

// operator lets a second person in holding operator in the default environment
// only, and returns their sign-in.
func (h *harness) operator(t *testing.T) authResponse {
	t.Helper()
	ctx := context.Background()
	admin := h.signIn(t, "provider|admin", "admin@example.com")
	if _, err := h.users.Create(ctx, "op@example.com", "", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	op := h.signIn(t, "provider|op", "op@example.com")
	if err := h.users.Grant(ctx, op.User.ID, user.RoleOperator, &admin.User.ID); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	return h.signIn(t, "provider|op", "op@example.com")
}

// A deployment's token names the one environment it runs in, and carries
// nothing for any other: a compromised pod reaches no further than that.
func TestAMachineTokenIsBoundToItsEnvironment(t *testing.T) {
	h := newHarness(t)
	admin := h.signIn(t, "provider|admin", "admin@example.com")

	_, private := h.machineIn(t, admin.Token, "staging", AccessOperator)
	if private.Environment != "staging" {
		t.Errorf("environment = %q, want staging", private.Environment)
	}
	if private.EnvRoles != nil {
		t.Errorf("a machine token carries envRoles %v", private.EnvRoles)
	}
	for _, r := range private.Roles {
		if r == user.RoleAdmin {
			t.Fatal("an admin's deployment carries admin")
		}
	}
}

// The default environment has two spellings, and a token carries one.
func TestTheDefaultEnvironmentIsNamedByOmission(t *testing.T) {
	h := newHarness(t)
	admin := h.signIn(t, "provider|admin", "admin@example.com")

	for _, name := range []string{"", user.DefaultEnvironment} {
		if _, private := h.machineIn(t, admin.Token, name); private.Environment != "" {
			t.Errorf("environment %q minted as %q, want the empty spelling", name, private.Environment)
		}
	}
}

// Deploying in an environment is judged by what the caller holds there. An
// operator in the default environment holds nothing in staging, so may lend a
// deployment there nothing — not even the basic identity.
func TestDeployingIsJudgedInTheTargetEnvironment(t *testing.T) {
	h := newHarness(t)
	op := h.operator(t)

	if rec := h.postMachineIn(t, "Bearer "+op.Token, "dep-1", ""); rec.Code != http.StatusOK {
		t.Fatalf("operator in the default = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if rec := h.postMachineIn(t, "Bearer "+op.Token, "dep-1", "staging", AccessOperator); rec.Code != http.StatusForbidden {
		t.Errorf("operator access in staging = %d (%s), want 403", rec.Code, rec.Body.String())
	}
	if rec := h.postMachineIn(t, "Bearer "+op.Token, "dep-1", "staging"); rec.Code != http.StatusForbidden {
		t.Errorf("a basic identity in staging = %d (%s), want 403", rec.Code, rec.Body.String())
	}
}

// A typo must not mint a credential that wakes up the day somebody creates an
// environment by that name.
func TestAnUnknownEnvironmentIsRefused(t *testing.T) {
	h := newHarness(t)
	admin := h.signIn(t, "provider|admin", "admin@example.com")
	if rec := h.postMachineIn(t, "Bearer "+admin.Token, "dep-1", "stagign"); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown environment = %d (%s), want 400", rec.Code, rec.Body.String())
	}
}

// Renewal keeps the environment and the access exactly; it never widens either.
func TestARenewalKeepsTheEnvironment(t *testing.T) {
	h := newHarness(t)
	admin := h.signIn(t, "provider|admin", "admin@example.com")
	token, _ := h.machineIn(t, admin.Token, "staging", AccessDeveloper)

	rec := h.postRefresh(t, "Bearer "+token)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/refresh = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var renewed authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &renewed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_, private := h.claimsOf(t, renewed.Token)
	if private.Environment != "staging" {
		t.Errorf("environment after renewal = %q, want staging", private.Environment)
	}
	if len(private.Roles) != 2 || private.EnvRoles != nil {
		t.Errorf("after renewal: roles %v, envRoles %v; want runtime and developer only",
			private.Roles, private.EnvRoles)
	}
}

// A machine token minted before environments existed carries no environment
// claim, and renews bound to the default — never to nothing.
func TestALegacyMachineTokenRenewsInTheDefault(t *testing.T) {
	h := newHarness(t)
	admin := h.signIn(t, "provider|admin", "admin@example.com")
	legacy, err := h.signing.Mint(context.Background(), admin.User.ID, map[string]any{
		"roles":      []user.Role{user.RoleRuntime},
		"deployment": "dep-1",
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	rec := h.postRefresh(t, "Bearer "+legacy.Value)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/refresh = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var renewed authResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &renewed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, private := h.claimsOf(t, renewed.Token); private.Environment != "" || private.Deployment != "dep-1" {
		t.Errorf("renewed legacy token: %+v", private)
	}
}

// A key bound to staging buys a token bound to staging, and that token cannot
// lend a deployment an identity anywhere else.
func TestAKeyTokenIsBoundToTheKeysEnvironment(t *testing.T) {
	h := newHarness(t)
	admin := h.signIn(t, "provider|admin", "admin@example.com")
	staging := "staging"
	h.keys[testKey] = apikey.APIKey{
		ID: "key-1", UserID: admin.User.ID, Roles: []user.Role{user.RoleCICD}, Environment: &staging,
	}

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
	if private.Environment != "staging" || private.EnvRoles != nil {
		t.Errorf("key token: environment %q, envRoles %v", private.Environment, private.EnvRoles)
	}

	if rec := h.postMachineIn(t, "Bearer "+got.Token, "dep-1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("a staging key lending in the default = %d (%s), want 403", rec.Code, rec.Body.String())
	}
	if rec := h.postMachineIn(t, "Bearer "+got.Token, "dep-1", "staging"); rec.Code != http.StatusOK {
		t.Errorf("a staging key lending in staging = %d (%s), want 200", rec.Code, rec.Body.String())
	}
}

// A key speaks for what its owner may delegate in the key's environment. An
// operator in the default environment holds nothing in staging, so a staging key
// of theirs buys nothing.
func TestAKeyIsNarrowedInItsOwnEnvironment(t *testing.T) {
	h := newHarness(t)
	op := h.operator(t)
	staging := "staging"
	h.keys[testKey] = apikey.APIKey{
		ID: "key-1", UserID: op.User.ID, Roles: []user.Role{user.RoleOperator}, Environment: &staging,
	}
	if rec := h.post(t, "Bearer "+testKey); rec.Code != http.StatusForbidden {
		t.Errorf("exchanging a staging key of a default-only operator = %d (%s), want 403",
			rec.Code, rec.Body.String())
	}
}

// Without a way to ask which environments exist, only the default is one.
func TestWithoutEnvironmentsOnlyTheDefaultMints(t *testing.T) {
	svc := &Service{}
	if err := svc.environmentExists(context.Background(), "staging"); err == nil {
		t.Error("an environment was accepted with nothing to check it against")
	}
	if err := svc.environmentExists(context.Background(), ""); err != nil {
		t.Errorf("the default was refused: %v", err)
	}
}

// stagingOperator lets a second person in holding operator and developer in
// staging and nothing in the default, and returns their sign-in.
func (h *harness) stagingOperator(t *testing.T) authResponse {
	t.Helper()
	ctx := context.Background()
	admin := h.signIn(t, "provider|admin", "admin@example.com")
	if _, err := h.users.Create(ctx, "stage@example.com", "", nil, nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	stage := h.signIn(t, "provider|stage", "stage@example.com")
	for _, r := range []user.Role{user.RoleOperator, user.RoleDeveloper} {
		if err := h.users.GrantIn(ctx, stage.User.ID, "staging", r, &admin.User.ID); err != nil {
			t.Fatalf("GrantIn: %v", err)
		}
	}
	return h.signIn(t, "provider|stage", "stage@example.com")
}

// A person's token carries what they hold in every environment, so switching
// needs no new token.
func TestAPersonsTokenCarriesTheirRolesInEveryEnvironment(t *testing.T) {
	h := newHarness(t)
	stage := h.stagingOperator(t)
	var private platformClaims
	if _, err := h.signing.Verify(context.Background(), stage.Token, &private); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(private.Roles) != 0 {
		t.Errorf("default roles = %v, want none", private.Roles)
	}
	if got := private.EnvRoles["staging"]; len(got) != 2 {
		t.Errorf("envRoles[staging] = %v, want operator and developer", got)
	}
	if len(stage.User.EnvRoles["staging"]) != 2 {
		t.Errorf("the response's user carries envRoles %v", stage.User.EnvRoles)
	}
}

// Somebody who operates staging and nothing else may deploy there, and only
// there.
func TestAStagingOperatorDeploysInStagingOnly(t *testing.T) {
	h := newHarness(t)
	stage := h.stagingOperator(t)

	_, private := h.machineIn(t, stage.Token, "staging", AccessOperator, AccessDeveloper)
	if private.Environment != "staging" || len(private.Roles) != 3 {
		t.Errorf("staging machine token: environment %q, roles %v", private.Environment, private.Roles)
	}
	if rec := h.postMachineIn(t, "Bearer "+stage.Token, "dep-1", ""); rec.Code != http.StatusForbidden {
		t.Errorf("deploying in the default = %d (%s), want 403", rec.Code, rec.Body.String())
	}
}

// A staging key of somebody who operates staging speaks for that, there.
func TestAStagingKeyOfAStagingOperator(t *testing.T) {
	h := newHarness(t)
	stage := h.stagingOperator(t)
	staging := "staging"
	h.keys[testKey] = apikey.APIKey{
		ID: "key-1", UserID: stage.User.ID, Roles: []user.Role{user.RoleOperator}, Environment: &staging,
	}
	if got := h.exchangedRoles(t); len(got) != 1 || got[0] != user.RoleOperator {
		t.Errorf("exchanged roles = %v, want operator", got)
	}
}
