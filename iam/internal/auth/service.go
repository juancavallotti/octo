package auth

import (
	"context"
	"fmt"

	"github.com/juancavallotti/octo/iam/internal/role"
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
	}
	minter interface {
		Mint(ctx context.Context, subject string, private any) (signing.Token, error)
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
	Roles []role.Role `json:"roles"`
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

	roles := u.Roles
	if roles == nil {
		// A user who has been granted nothing gets an empty list and not a null, so
		// a verifier never has to distinguish two encodings of the same fact.
		roles = []role.Role{}
	}
	token, err := s.minter.Mint(ctx, u.ID, platformClaims{
		Email: u.Email,
		Name:  u.Name,
		Roles: roles,
	})
	if err != nil {
		return Result{}, fmt.Errorf("auth: mint token: %w", err)
	}

	return Result{Token: token, User: u}, nil
}
