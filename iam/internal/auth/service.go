package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

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
		Verify(ctx context.Context, raw string, allowExpiredFor time.Duration) (jwt.Claims, error)
	}
)

// Service performs the exchange.
type Service struct {
	verifier verifier
	users    users
	minter   minter

	// refreshGrace is how long after expiry a platform token can still be traded
	// for a fresh one. See Refresh.
	refreshGrace time.Duration
}

// DefaultRefreshGrace is how long past its expiry a platform token is still
// accepted by Refresh.
//
// Ten minutes, and the number is a compromise between two annoyances. Too short
// and somebody who stepped away from a tab is signed out for it; too long and an
// expired token stays a credential well after the moment it was supposed to stop
// being one. The real bound on the whole chain is the session lifetime the
// platform sets on its cookie, not this.
const DefaultRefreshGrace = 10 * time.Minute

// NewService returns a Service. Any collaborator being absent is ErrNotConfigured
// rather than a nil dereference later: the exchange needs all three, and an
// install missing the identity provider is a supported way to run — it simply
// cannot mint.
func NewService(v verifier, u users, m minter, refreshGrace time.Duration) (*Service, error) {
	if v == nil || u == nil || m == nil {
		return nil, ErrNotConfigured
	}
	if refreshGrace <= 0 {
		refreshGrace = DefaultRefreshGrace
	}
	return &Service{verifier: v, users: u, minter: m, refreshGrace: refreshGrace}, nil
}

// platformClaims is what a platform token carries beyond the registered claims
// the signing service stamps. Roles are the reason the token exists: they are
// what a downstream service authorizes on, and putting them in the token is what
// spares every one of those services a call back here on every request.
type platformClaims struct {
	Email string      `json:"email"`
	Name  string      `json:"name"`
	Roles []user.Role `json:"roles"`
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
	claims, err := s.minter.Verify(ctx, rawToken, s.refreshGrace)
	if err != nil {
		// Only a token this service did not mint, or minted too long ago, is the
		// caller's problem. Verify also reads the keyset from the database on its
		// way through, and that failing says nothing at all about the token.
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

	token, err := s.mint(ctx, u)
	if err != nil {
		return Result{}, err
	}
	return Result{Token: token, User: u}, nil
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
