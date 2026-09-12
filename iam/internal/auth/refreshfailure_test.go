package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/juancavallotti/octo/iam/internal/signing"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// What a refresh failure is answered with decides whether an installation stays
// signed in.
//
// A platform told "unauthenticated" clears the session and sends everybody back
// to the identity provider. A platform told "unavailable" keeps the credential it
// already holds and carries on. Both of this exchange's collaborators read the
// database on their way through, so answering their failures as bad tokens turns
// a moment's trouble at Postgres into an installation-wide sign-out — every
// session that happened to be inside its renewal window at the time.

// stubMinter answers Verify with whatever the case set up.
type stubMinter struct{ err error }

func (s stubMinter) Mint(context.Context, string, any) (signing.Token, error) {
	return signing.Token{}, nil
}

func (s stubMinter) Verify(context.Context, string, time.Duration) (jwt.Claims, error) {
	return jwt.Claims{Subject: "user-1"}, s.err
}

// stubUsers answers Get with whatever the case set up.
type stubUsers struct{ err error }

func (s stubUsers) SignIn(context.Context, string, string, string) (user.User, error) {
	return user.User{}, nil
}

func (s stubUsers) Get(context.Context, string) (user.User, error) {
	return user.User{ID: "user-1"}, s.err
}

func TestRefreshTellsABadTokenFromAServiceThatCannotAnswer(t *testing.T) {
	// What a database failure looks like coming out of either collaborator: a
	// wrapped error that is neither "not our token" nor "no such user".
	dbDown := fmt.Errorf("user repo: get: %w", errors.New("dial tcp: connection refused"))

	tests := []struct {
		name    string
		minter  error
		users   error
		want    error
		because string
	}{
		{
			name: "a token this service never minted", minter: signing.ErrNotOurToken,
			want:    ErrUnauthenticated,
			because: "the caller presented something we cannot verify",
		},
		{
			name: "the keyset could not be read", minter: dbDown,
			want:    ErrUnavailable,
			because: "reading our own keys failed, which says nothing about the token",
		},
		{
			name: "the account is gone", users: user.ErrNotFound,
			want:    ErrUnauthenticated,
			because: "the token outlived the account it speaks for",
		},
		{
			name: "the user could not be looked up", users: dbDown,
			want:    ErrUnavailable,
			because: "the database was unreachable, and the session is still good",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				minter:       stubMinter{err: tt.minter},
				users:        stubUsers{err: tt.users},
				refreshGrace: DefaultRefreshGrace,
			}
			_, err := svc.Refresh(context.Background(), "a.b.c")
			if !errors.Is(err, tt.want) {
				t.Errorf("Refresh returned %v, want %v — %s", err, tt.want, tt.because)
			}
		})
	}
}

// And the status that carries it: 401 is what clears a session, so a fault on
// this side must not wear one.
func TestAServiceThatCannotAnswerIsA503(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.writeError(rec, fmt.Errorf("%w: %w", ErrUnavailable, errors.New("connection refused")))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — a 401 here signs every session out", rec.Code)
	}
}
