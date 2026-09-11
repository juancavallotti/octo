package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/juancavallotti/octo/iam/internal/signing"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// The three collaborators, each declared here in the consumer and each one
// method wide, so the exchange can be tested without a provider, a database or a
// keyset. *Verifier, *user.Service and *signing.Service satisfy them
// structurally.
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
)

// Service performs the exchange.
type Service struct {
	verifier verifier
	users    users
	minter   minter
}

// NewService returns a Service. Any collaborator being absent is ErrNotConfigured
// rather than a nil dereference later: the exchange needs all three, and an
// install missing the identity provider is a supported way to run — it simply
// cannot mint.
func NewService(v verifier, u users, m minter) (*Service, error) {
	if v == nil || u == nil || m == nil {
		return nil, ErrNotConfigured
	}
	return &Service{verifier: v, users: u, minter: m}, nil
}

// platformClaims is what a platform token carries beyond the registered claims
// the signing service stamps. Roles are the reason the token exists: they are
// what a downstream service authorizes on, and putting them in the token is what
// spares every one of those services a call back here on every request.
type platformClaims struct {
	Email string      `json:"email"`
	Name  string      `json:"name"`
	Roles []user.Role `json:"roles"`
	// Deployment names the deployed integration a machine token was minted for,
	// and is empty on a person's token. It is what tells the two apart on the way
	// back in — see Refresh.
	Deployment string `json:"deployment,omitempty"`
}

// Result is a completed exchange: the minted token and the user it speaks for.
type Result struct {
	Token signing.Token
	User  user.User
}

// Exchange verifies rawToken with the identity provider and returns a platform
// token for whoever it identifies.
//
// The subject of the minted token is the octo user id and not the provider's
// `sub`. That is the whole conversion: everything downstream refers to a
// principal by an identifier this platform owns, which survives the provider
// changing an account's email — or the platform changing provider.
func (s *Service) Exchange(ctx context.Context, rawToken string) (Result, error) {
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

	token, err := s.mint(ctx, u)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: u}, nil
}

// Refresh trades a platform token this service minted for a fresh one, without
// going back to the identity provider.
//
// It exists because the two lifetimes do not match. A platform token lives an
// hour; a person's session lives a working day. Sending them back to the provider
// every hour would be absurd, and the alternative — storing a long-lived provider
// credential in the session so we can re-exchange it — is a worse thing to hold
// than the short-lived token we already have.
//
// A token that expired within the grace window is still accepted. That is the
// case this is for: somebody left a tab open over lunch. Beyond the window there
// is no recovery here and the platform sends them to sign in again.
//
// The roles are re-read from the database rather than copied across from the old
// token, and that is the whole reason this is not simply a re-signing. It is what
// makes a revoked role take effect within one token lifetime instead of at the
// next sign-in. `last_login_at` is deliberately not touched: this is not a
// sign-in, and treating it as one would make the column mean "was recently using
// the platform" rather than what it says.
func (s *Service) Refresh(ctx context.Context, rawToken string) (Result, error) {
	var private platformClaims
	claims, err := s.minter.Verify(ctx, rawToken, &private)
	if err != nil && !renewableWhileExpired(err, private) {
		// Only a token this service did not mint — or a person's, minted too long
		// ago — is the caller's problem. Verify also reads the keyset from the
		// database on its way through, and that failing says nothing at all about
		// the token, so it must not be answered as though the token were bad.
		if errors.Is(err, signing.ErrNotOurToken) {
			return Result{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	// The subject of a platform token is the octo user id, which is what makes
	// this a plain lookup rather than anything the provider has to answer for.
	u, err := s.users.Get(ctx, claims.Subject)
	if err != nil {
		// A user who has been deleted since the token was minted lands here, and a
		// refusal is the right answer: the token outlived the account. A database
		// that could not be reached is not that, and must not be answered as if it
		// were — see ErrUnavailable.
		if errors.Is(err, user.ErrNotFound) {
			return Result{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
		}
		return Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}

	// A machine token is renewed as a machine token. Re-minting it from the
	// owner's current roles would quietly promote a pod to whatever its owner may
	// do, which is the one thing a machine token is careful not to give it.
	//
	// Minted from the owner resolved above rather than by going back through
	// MintMachine, which would verify the presented token again — and would refuse
	// it, both because it is a machine's and because it may be inside the grace
	// window rather than still valid.
	//
	// It also does not re-ask whether the owner may still deploy, and that is
	// deliberate. The deployment was authorised when it was created; taking
	// somebody's operator role away should not quietly stop integrations that are
	// serving traffic. Stopping one is what deleting the deployment is for, and it
	// is a decision somebody should have to make on purpose.
	if private.Deployment != "" {
		token, err := s.mintMachine(ctx, u, private.Deployment)
		if err != nil {
			return Result{}, err
		}
		return Result{Token: token, User: u}, nil
	}

	token, err := s.mint(ctx, u)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: u}, nil
}

// renewableWhileExpired reports whether an otherwise-good token may be renewed
// despite having expired.
//
// Only a machine token, and always. It is a deployment's standing credential
// rather than a session: a pod that has been idle or switched off for a month
// must still be able to trade its token in, and an expiry it cannot act on would
// strand it with no way back. Every other check stood — the signature, the
// issuer, the audience — so this forgives an old token, never a forged one.
//
// A person's token is not renewable once expired. They sign in again.
func renewableWhileExpired(err error, private platformClaims) bool {
	return errors.Is(err, signing.ErrExpired) && private.Deployment != ""
}

// mint stamps a platform token for u. Shared by the exchange and the refresh so
// the two cannot drift into carrying different claims for the same person.
func (s *Service) mint(ctx context.Context, u user.User) (signing.Token, error) {
	roles := u.Roles
	if roles == nil {
		// A user who has been granted nothing gets an empty list and not a null, so
		// a verifier never has to distinguish two encodings of the same fact.
		roles = []user.Role{}
	}
	token, err := s.minter.Mint(ctx, u.ID, platformClaims{
		Email: u.Email,
		Name:  u.Name,
		Roles: roles,
	})
	if err != nil {
		return signing.Token{}, fmt.Errorf("auth: mint token: %w", err)
	}
	return token, nil
}

// MintMachine issues a token for a deployed integration, on the authority of the
// person deploying it.
//
// A running integration has to reach the platform's API — its key/value store,
// its frozen resources, its agent memory — and has no way to sign in. So the
// person who deploys it lends it an identity: rawToken is their platform token,
// and what comes back speaks for them.
//
// Two things about the token it gets, and they are the whole design:
//
//   - Its subject is that person, so everything the platform scopes to a user
//     scopes the same way for their deployment. A pod cannot reach another
//     person's data, because as far as the API is concerned it is not another
//     person.
//   - Its only role is platform:runtime, whatever the person holds. An
//     administrator's deployment is not an administrator. This is the difference
//     between lending an identity and handing over an account.
//
// And it is only issued to somebody who may deploy in the first place. Without
// that check this would be a way for anyone with an account to hand themselves
// the runtime role — which reaches a deployment's key/value store and its frozen
// resources — by claiming to be deploying something. A read-only account asking
// for one is not a deployment, it is an escalation.
//
// It lives exactly as long as a person's token and is renewed the same way, which
// is deliberate: a longer-lived one could outlive the key that signed it, since
// the keyset only keeps a key published for one token lifetime past its
// retirement.
func (s *Service) MintMachine(ctx context.Context, rawToken, deployment string) (Result, error) {
	deployment = strings.TrimSpace(deployment)
	if deployment == "" {
		return Result{}, fmt.Errorf("%w: a deployment is required", user.ErrInvalid)
	}

	owner, err := s.owner(ctx, rawToken)
	if err != nil {
		return Result{}, err
	}
	if !mayDeploy(owner) {
		return Result{}, fmt.Errorf(
			"%w: lending an identity to a deployment requires %s or %s",
			ErrForbidden, user.RoleOperator, user.RoleAdmin)
	}

	token, err := s.mintMachine(ctx, owner, deployment)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: owner}, nil
}

// mintMachine stamps the token itself. Shared by the first mint and every
// renewal, so the two cannot drift into describing the same pod differently.
func (s *Service) mintMachine(
	ctx context.Context, owner user.User, deployment string,
) (signing.Token, error) {
	token, err := s.minter.Mint(ctx, owner.ID, platformClaims{
		Email:      owner.Email,
		Name:       owner.Name,
		Roles:      []user.Role{user.RoleRuntime},
		Deployment: deployment,
	})
	if err != nil {
		return signing.Token{}, fmt.Errorf("auth: mint machine token: %w", err)
	}
	return token, nil
}

// owner verifies a platform token and returns the person it speaks for, refusing
// one that is itself a machine's.
//
// A pod must not be able to mint another pod a token: that would make a single
// leaked machine credential renewable into an unbounded family of them, none of
// which any person ever authorised.
func (s *Service) owner(ctx context.Context, rawToken string) (user.User, error) {
	var claims platformClaims
	verified, err := s.minter.Verify(ctx, rawToken, &claims)
	if err != nil {
		return user.User{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	if claims.Deployment != "" {
		return user.User{}, fmt.Errorf(
			"%w: a machine token cannot mint another", ErrUnauthenticated)
	}
	u, err := s.users.Get(ctx, verified.Subject)
	if err != nil {
		return user.User{}, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	return u, nil
}

// mayDeploy reports whether u is somebody who runs things here, and so somebody
// whose deployments may be given an identity.
//
// The two roles that describe running deployments, named here rather than
// derived from what the orchestrator will allow: this service cannot see that
// policy, and the question it is actually answering is narrower — is this a
// person who deploys, or a person asking for a credential they have no use for.
func mayDeploy(u user.User) bool {
	return u.HasRole(user.RoleAdmin) || u.HasRole(user.RoleOperator)
}
