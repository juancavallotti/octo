package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/juancavallotti/octo/iam/internal/signing"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// The two rules for a platform token arriving at POST /auth/refresh: a person's
// is dead once it expires, and a deployment's never is. Driven through a stub
// keyset rather than a real one, because the interesting input is "a token that
// is ours and expired", which a real signer cannot be asked for.

// stubMinter answers Verify with whatever the case sets up and records what it
// was asked to mint.
type stubMinter struct {
	claims  jwt.Claims
	private platformClaims
	err     error

	minted []platformClaims
}

func (s *stubMinter) Verify(_ context.Context, _ string, private any) (jwt.Claims, error) {
	if dest, ok := private.(*platformClaims); ok {
		*dest = s.private
	}
	return s.claims, s.err
}

func (s *stubMinter) Mint(_ context.Context, _ string, private any) (signing.Token, error) {
	if p, ok := private.(platformClaims); ok {
		s.minted = append(s.minted, p)
	}
	return signing.Token{Value: "fresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// expired is the error signing returns for a token that failed only that check.
func expired() error {
	return errors.Join(signing.ErrNotOurToken, signing.ErrExpired)
}

// serviceWith returns a service whose store holds one user, and that user's id —
// which is what a platform token's subject is, so every case stamps it on the
// claims its stub verifier hands back.
func serviceWith(t *testing.T, m *stubMinter) (*Service, string) {
	t.Helper()
	repo := newMemUsers()
	owner, err := repo.Create(context.Background(), "subject", "owner@example.com", "Owner")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc, err := NewService(stubVerifier{}, user.NewService(repo), m)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, owner.ID
}

type stubVerifier struct{}

func (stubVerifier) Verify(context.Context, string) (Identity, error) {
	return Identity{}, errors.New("not used")
}

// A pod that has been switched off for a month still has a way back. Nothing
// re-mints its seed, so an expiry it could not act on would strand it for good.
func TestRefreshRenewsAMachineTokenHoweverLongAgoItExpired(t *testing.T) {
	m := &stubMinter{private: platformClaims{Deployment: "dep-1"}, err: expired()}
	svc, id := serviceWith(t, m)
	m.claims = jwt.Claims{Subject: id}

	result, err := svc.Refresh(context.Background(), "an ancient machine token")
	if err != nil {
		t.Fatalf("Refresh() of an expired machine token: %v", err)
	}
	if result.Token.Value != "fresh" {
		t.Errorf("token = %q, want a freshly minted one", result.Token.Value)
	}
	if len(m.minted) != 1 {
		t.Fatalf("minted %d tokens, want 1", len(m.minted))
	}
	if got := m.minted[0].Deployment; got != "dep-1" {
		t.Errorf("renewed for deployment %q, want the one the token named", got)
	}
	if roles := m.minted[0].Roles; len(roles) != 1 || roles[0] != user.RoleRuntime {
		t.Errorf("roles = %v, want only %s", roles, user.RoleRuntime)
	}
}

// A person's expired token is not a credential. They sign in again.
func TestRefreshRefusesAnExpiredUserToken(t *testing.T) {
	m := &stubMinter{err: expired()}
	svc, id := serviceWith(t, m)
	m.claims = jwt.Claims{Subject: id}

	if _, err := svc.Refresh(context.Background(), "an expired session"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Refresh() of an expired user token = %v, want ErrUnauthenticated", err)
	}
	if len(m.minted) != 0 {
		t.Errorf("minted %d tokens for an expired session, want none", len(m.minted))
	}
}

// Expiry is the only thing forgiven. A machine token that fails any other check
// is refused like anything else — it is not ours.
func TestRefreshRefusesAMachineTokenThatIsNotOurs(t *testing.T) {
	m := &stubMinter{private: platformClaims{Deployment: "dep-1"}, err: signing.ErrNotOurToken}
	svc, id := serviceWith(t, m)
	m.claims = jwt.Claims{Subject: id}

	if _, err := svc.Refresh(context.Background(), "forged"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("Refresh() of an unverifiable machine token = %v, want ErrUnauthenticated", err)
	}
	if len(m.minted) != 0 {
		t.Errorf("minted %d tokens for a token that is not ours, want none", len(m.minted))
	}
}
