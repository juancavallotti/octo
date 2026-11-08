package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/juancavallotti/octo/iam/internal/apikey"
	"github.com/juancavallotti/octo/iam/internal/signing"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// The four collaborators, declared here in the consumer so the exchange can be
// tested without a provider, a database or a keyset.
type (
	verifier interface {
		Verify(ctx context.Context, rawToken string) (Identity, error)
	}
	users interface {
		SignIn(ctx context.Context, subject, email, name string) (user.User, error)
		Get(ctx context.Context, id string) (user.User, error)
	}
	minter interface {
		Mint(ctx context.Context, subject string, private any) (signing.Token, error)
		Verify(ctx context.Context, raw string, private any) (jwt.Claims, error)
	}
	keys interface {
		Verify(ctx context.Context, plaintext string) (apikey.APIKey, error)
	}
	environments interface {
		Exists(ctx context.Context, name string) (bool, error)
	}
)

// Service performs the exchange.
type Service struct {
	verifier verifier
	users    users
	minter   minter
	keys     keys
	// environments answers whether a name a machine token is asked to be bound
	// to is an environment. Nil allows only the default environment.
	environments environments
}

// Option configures a Service beyond its four required collaborators.
type Option func(*Service)

// WithEnvironments lets machine tokens be bound to environments other than the
// default, checked against e.
func WithEnvironments(e environments) Option {
	return func(s *Service) { s.environments = e }
}

// NewService returns a Service. Any collaborator being absent is ErrNotConfigured
// rather than a nil dereference later: the exchange needs all four, and an
// install missing the identity provider is a supported way to run — it simply
// cannot mint.
func NewService(v verifier, u users, m minter, k keys, opts ...Option) (*Service, error) {
	if v == nil || u == nil || m == nil || k == nil {
		return nil, ErrNotConfigured
	}
	s := &Service{verifier: v, users: u, minter: m, keys: k}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// platformClaims is what a platform token carries beyond the registered claims
// the signing service stamps. The roles travel in the token so that verifying it
// is enough to authorize on them, with no call back here.
type platformClaims struct {
	// Email and Name describe the person a token speaks for, and are omitted
	// entirely from a machine's — see mintMachine. A JWT is not encrypted, so a
	// machine token carries no address nothing consumes.
	Email string      `json:"email,omitempty"`
	Name  string      `json:"name,omitempty"`
	Roles []user.Role `json:"roles"`
	// Deployment names the deployed integration a machine token was minted for,
	// and is empty on a person's token. It is what tells the two apart on the way
	// back in — see Refresh.
	Deployment string `json:"deployment,omitempty"`
	// APIKey names the key a token was exchanged for, and is empty on a token from
	// a sign-in. Refresh refuses such a token, so revoking the key ends it within
	// one token lifetime.
	APIKey string `json:"apiKey,omitempty"`
	// EnvRoles is what a person holds in each environment other than the
	// default, whose roles are Roles. Only a person's token carries it.
	EnvRoles map[string][]user.Role `json:"envRoles,omitempty"`
	// Environment is the one environment a bound token acts in, empty for the
	// default. A token is bound when it is a machine's (Deployment) or came from
	// a key (APIKey); its Roles are then its roles in that environment and it has
	// none in any other. Binding is read off those two claims and never off this
	// one, so a bound token minted before environments existed — which carries no
	// Environment — is bound to the default rather than to nothing.
	Environment string `json:"environment,omitempty"`
}

// Result is a completed exchange: the minted token and the user it speaks for.
type Result struct {
	Token signing.Token
	User  user.User
}

// Exchange verifies rawToken and returns a platform token for whoever it
// identifies. rawToken is either the identity provider's token or one of the
// caller's API keys; the two arrive at the same endpoint so that anything taking
// a bearer from a person can hand it on without knowing which it holds.
//
// The subject of the minted token is the octo user id and not the provider's
// `sub`, so a principal is named by an identifier that survives the provider
// changing an account's email, or the install changing provider.
func (s *Service) Exchange(ctx context.Context, rawToken string) (Result, error) {
	if apikey.IsKey(rawToken) {
		return s.exchangeKey(ctx, rawToken)
	}

	identity, err := s.verifier.Verify(ctx, rawToken)
	if err != nil {
		return Result{}, err
	}

	// Provisions the user on first sight, refreshes their profile on every later
	// sign-in, and makes the very first one an admin.
	u, err := s.users.SignIn(ctx, identity.Subject, identity.Email, identity.Name)
	if err != nil {
		return Result{}, fmt.Errorf("auth: resolve user: %w", err)
	}

	token, err := s.mint(ctx, u, "", "")
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: u}, nil
}

// exchangeKey mints a platform token for the owner of an API key.
//
// The token carries the key's roles that the owner may still delegate now, so a
// key never outranks the person it belongs to and shrinks when they are demoted. A key
// left with none is refused rather than answered with an empty token. The result's
// user carries those same roles: they are what this credential may do.
// `last_login_at` is not touched: presenting a key is not a sign-in.
func (s *Service) exchangeKey(ctx context.Context, rawKey string) (Result, error) {
	k, err := s.keys.Verify(ctx, rawKey)
	if err != nil {
		if errors.Is(err, apikey.ErrInvalidToken) || errors.Is(err, apikey.ErrExpired) {
			return Result{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	u, err := s.users.Get(ctx, k.UserID)
	if err != nil {
		if errors.Is(err, user.ErrNotFound) {
			return Result{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	// What the owner may delegate is asked in the key's own environment: a key
	// bound to staging speaks only for what its owner may do there.
	environment := ""
	if k.Environment != nil {
		environment = *k.Environment
	}
	scoped := u.In(environment)
	var roles []user.Role
	for _, r := range k.Roles {
		if scoped.MayDelegate(r) {
			roles = append(roles, r)
		}
	}
	if len(roles) == 0 {
		return Result{}, fmt.Errorf(
			"%w: this API key's roles are no longer held by its owner; mint a new key", ErrForbidden)
	}
	u.Roles = roles
	u.EnvRoles = nil

	token, err := s.mint(ctx, u, k.ID, environment)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: u}, nil
}

// Refresh trades a platform token this service minted for a fresh one, without
// going back to the identity provider.
//
// It exists because the two lifetimes do not match: a platform token lives an
// hour, a session lives a working day.
//
// A token that expired within the grace window is still accepted; beyond the
// window there is no recovery here and the caller must sign in again.
//
// The roles are re-read from the database rather than copied across from the old
// token, so a revoked role takes effect within one token lifetime rather than at
// the next sign-in. `last_login_at` is not touched: this is not a sign-in.
func (s *Service) Refresh(ctx context.Context, rawToken string) (Result, error) {
	var private platformClaims
	claims, err := s.minter.Verify(ctx, rawToken, &private)
	if err != nil && !renewableWhileExpired(err, private) {
		// Only a token this service did not mint — or a person's, minted too long
		// ago — is the caller's problem. Verify also reads the keyset from the
		// database, and that failing says nothing about the token.
		if errors.Is(err, signing.ErrNotOurToken) {
			return Result{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	// The subject of a platform token is the octo user id, so this is a plain
	// lookup with nothing for the provider to answer.
	u, err := s.users.Get(ctx, claims.Subject)
	if err != nil {
		// A user deleted since the token was minted lands here, and is refused: the
		// token outlived the account. An unreachable database is not that — see
		// ErrUnavailable.
		if errors.Is(err, user.ErrNotFound) {
			return Result{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	// A token from an API key is renewed by presenting the key again. Renewing it
	// here would let it outlive the key's revocation, since nothing on this path
	// asks whether the key still stands.
	if private.APIKey != "" {
		return Result{}, fmt.Errorf(
			"%w: a token exchanged for an API key is renewed by exchanging the key again",
			ErrUnauthenticated)
	}

	// A machine token is renewed as a machine token: re-minting it from the owner's
	// current roles would promote a deployment to whatever its owner may do.
	//
	// Minted from the owner resolved above rather than through MintMachine, which
	// would verify the presented token again and refuse it — both because it is a
	// machine's and because it may be inside the grace window.
	//
	// Whether the owner may still deploy is not re-asked: the deployment was
	// authorised when it was created, and revoking a role does not stop an
	// integration already serving traffic.
	//
	// The access comes back off the presented token, the only record of what this
	// deployment was lent, and is read through accessOf rather than copied, so a
	// renewal cannot carry anything this service would not mint today.
	//
	// The environment comes back off the token too, and is never widened: a
	// deployment in staging renews as a deployment in staging, whatever its owner
	// holds anywhere now.
	if private.Deployment != "" {
		token, err := s.mintMachine(ctx, u, private.Deployment, private.Environment, accessOf(private.Roles))
		if err != nil {
			return Result{}, err
		}
		return Result{Token: token, User: u}, nil
	}

	token, err := s.mint(ctx, u, "", "")
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: u}, nil
}

// renewableWhileExpired reports whether an otherwise-good token may be renewed
// despite having expired.
//
// Only a machine token, and always: it is a deployment's standing credential
// rather than a session, and a deployment idle for a month must still be able to
// trade its token in. Every other check stood — the signature, the issuer, the
// audience — so this forgives an old token, never a forged one.
func renewableWhileExpired(err error, private platformClaims) bool {
	return errors.Is(err, signing.ErrExpired) && private.Deployment != ""
}

// mint stamps a platform token for u, naming the API key it was exchanged for
// when there is one, and the environment that key is bound to. Shared by both
// exchanges and the refresh so they cannot drift into carrying different claims
// for the same person.
//
// A key's token carries u as the exchange narrowed it — the key's roles in its
// environment — and no EnvRoles, so it has nothing anywhere else.
func (s *Service) mint(ctx context.Context, u user.User, keyID, environment string) (signing.Token, error) {
	roles := u.Roles
	if roles == nil {
		// A user who has been granted nothing gets an empty list and not a null, so
		// a verifier never has to distinguish two encodings of the same fact.
		roles = []user.Role{}
	}
	claims := platformClaims{
		Email:  u.Email,
		Name:   u.Name,
		Roles:  roles,
		APIKey: keyID,
	}
	if keyID != "" {
		claims.Environment = environment
	} else if len(u.EnvRoles) > 0 {
		claims.EnvRoles = u.EnvRoles
	}
	token, err := s.minter.Mint(ctx, u.ID, claims)
	if err != nil {
		return signing.Token{}, fmt.Errorf("auth: mint token: %w", err)
	}
	return token, nil
}

// MintMachine issues a token for a deployed integration, on the authority of the
// person deploying it.
//
// A running integration has no way to sign in, so the person who deploys it lends
// it an identity: rawToken is their platform token, and what comes back speaks for
// them.
//
// Two properties of the token it gets:
//
//   - Its subject is that person, so anything scoped to a user scopes the same way
//     for their deployment and cannot reach another person's data.
//   - Its roles are what `access` asks for and never what the person holds. An
//     administrator's deployment is not an administrator.
//
// It is issued only to somebody who may deploy, or any account could hand itself
// the runtime role by claiming to be deploying something.
//
// It is bound to environment ("" or "octo" for the default) and confined to it:
// it carries no roles anywhere else, and both the right to deploy and the access
// it is lent are judged by what the caller holds in that environment alone. A
// compromised pod therefore reaches no further than the environment it runs in.
//
// It lives exactly as long as a person's token and is renewed the same way: a
// longer-lived one could outlive the key that signed it, since the keyset keeps a
// key published for only one token lifetime past its retirement.
func (s *Service) MintMachine(
	ctx context.Context, rawToken, deployment, environment string, access []Access,
) (Result, error) {
	deployment = strings.TrimSpace(deployment)
	if deployment == "" {
		return Result{}, fmt.Errorf("%w: a deployment is required", user.ErrInvalid)
	}
	if _, err := rolesFor(access); err != nil {
		return Result{}, err
	}
	environment = user.NormalizeEnvironment(strings.TrimSpace(environment))

	owner, claims, err := s.owner(ctx, rawToken)
	if err != nil {
		return Result{}, err
	}
	if err := s.environmentExists(ctx, environment); err != nil {
		return Result{}, err
	}
	scoped := owner.In(environment)
	if claims.APIKey != "" {
		// A key's token is bound, and lends only in the environment it is bound to.
		if claims.Environment != environment {
			return Result{}, fmt.Errorf(
				"%w: this API key is bound to environment %q", ErrForbidden, displayEnvironment(claims.Environment))
		}
		if err := keyMayLend(claims.Roles, access); err != nil {
			return Result{}, err
		}
	} else if !mayDeploy(scoped) {
		return Result{}, fmt.Errorf(
			"%w: lending an identity to a deployment requires %s or %s",
			ErrForbidden, user.RoleOperator, user.RoleAdmin)
	} else if !mayLend(scoped, access) {
		// The access is named and the caller's roles are not: they asked for this
		// much and may not have it, which is a thing they can act on.
		return Result{}, fmt.Errorf(
			"%w: this account may not lend a deployment %s access",
			ErrForbidden, describe(access))
	}

	token, err := s.mintMachine(ctx, owner, deployment, environment, access)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: owner}, nil
}

// environmentExists refuses a name that is not an environment. Without a way to
// ask, only the default is one.
func (s *Service) environmentExists(ctx context.Context, environment string) error {
	if environment == "" {
		return nil
	}
	if s.environments == nil {
		return fmt.Errorf("%w: environments are not available here", user.ErrInvalid)
	}
	ok, err := s.environments.Exists(ctx, environment)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !ok {
		return fmt.Errorf("%w: %q is not an environment", user.ErrInvalid, environment)
	}
	return nil
}

// displayEnvironment names an environment for a message, spelling the default.
func displayEnvironment(environment string) string {
	if environment == "" {
		return user.DefaultEnvironment
	}
	return environment
}

// mintMachine stamps the token itself. Shared by the first mint and every renewal,
// so the two cannot describe the same deployment differently.
func (s *Service) mintMachine(
	ctx context.Context, owner user.User, deployment, environment string, access []Access,
) (signing.Token, error) {
	roles, err := rolesFor(access)
	if err != nil {
		return signing.Token{}, err
	}
	// No Email, no Name. The subject still ties this token to the person who lent
	// it, which is what scopes what it reaches, but nothing reads an address off a
	// machine token and it should carry only what is consumed.
	token, err := s.minter.Mint(ctx, owner.ID, platformClaims{
		Roles:       roles,
		Deployment:  deployment,
		Environment: environment,
	})
	if err != nil {
		return signing.Token{}, fmt.Errorf("auth: mint machine token: %w", err)
	}
	return token, nil
}

// owner verifies a platform token and returns the person it speaks for, refusing
// one that is itself a machine's.
//
// A machine token must not be able to mint another, or one leaked credential
// becomes an unbounded family of them that no person authorised.
func (s *Service) owner(ctx context.Context, rawToken string) (user.User, platformClaims, error) {
	var claims platformClaims
	verified, err := s.minter.Verify(ctx, rawToken, &claims)
	if err != nil {
		return user.User{}, claims, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	if claims.Deployment != "" {
		return user.User{}, claims, fmt.Errorf(
			"%w: a machine token cannot mint another", ErrUnauthenticated)
	}
	u, err := s.users.Get(ctx, verified.Subject)
	if err != nil {
		return user.User{}, claims, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	return u, claims, nil
}

// keyMayLend decides for a token exchanged for an API key, whose roles are the
// key's own: it may lend a deployment an identity when it carries cicd or
// operator, and then only the basic one.
//
// A machine token renews without a key behind it, so whatever a key lends
// outlives the key's revocation. An access grant therefore takes a signed-in
// person.
func keyMayLend(roles []user.Role, access []Access) error {
	if !slices.Contains(roles, user.RoleCICD) && !slices.Contains(roles, user.RoleOperator) {
		return fmt.Errorf(
			"%w: an API key lends a deployment an identity only when it carries %s or %s",
			ErrForbidden, user.RoleCICD, user.RoleOperator)
	}
	if len(access) > 0 {
		return fmt.Errorf(
			"%w: an API key may lend a deployment only its basic identity; sign in to grant it %s access",
			ErrForbidden, describe(access))
	}
	return nil
}

// mayDeploy reports whether u is somebody who runs things here, and so somebody
// whose deployments may be given an identity.
//
// The two roles that describe running deployments. The question is narrow — is
// this a person who deploys — not whether any particular deployment will be
// allowed.
func mayDeploy(u user.User) bool {
	return u.HasRole(user.RoleAdmin) || u.HasRole(user.RoleOperator)
}
