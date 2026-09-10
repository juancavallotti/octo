package authz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeVerifier stands in for the keyset: it answers with whatever the case set
// up, so these cover what the guard does with an answer rather than how a
// signature is checked (which is the signing package's own business).
type fakeVerifier struct {
	claims jwt.Claims
	roles  []string
	err    error

	// window records the grace the guard asked for, so a case can assert it asked
	// for none.
	window time.Duration
}

func (f *fakeVerifier) Verify(
	_ context.Context, _ string, allowExpiredFor time.Duration, private any,
) (jwt.Claims, error) {
	f.window = allowExpiredFor
	if f.err != nil {
		return jwt.Claims{}, f.err
	}
	if dest, ok := private.(*principalClaims); ok {
		dest.Roles = f.roles
	}
	return f.claims, nil
}

// reached records whether the guarded handler ran, and what it saw.
func guarded(v verifier, roles ...string) (http.Handler, *Principal) {
	var seen Principal
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if p, err := FromContext(r.Context()); err == nil {
			seen = p
		}
	})
	return Require(v, roles...)(next), &seen
}

func call(h http.Handler, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	if bearer != "" {
		req.Header.Set("Authorization", bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequireAdmitsACallerHoldingTheRole(t *testing.T) {
	v := &fakeVerifier{claims: jwt.Claims{Subject: "user-1"}, roles: []string{"platform:admin"}}
	h, seen := guarded(v, "platform:admin")

	if rec := call(h, "Bearer good"); rec.Code != http.StatusOK {
		t.Fatalf("guarded route = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if seen.Subject != "user-1" {
		t.Errorf("the handler saw subject %q, want user-1", seen.Subject)
	}
	if !seen.HasRole("platform:admin") {
		t.Error("the handler saw a principal without the role it was admitted on")
	}
}

func TestRequireRefusesACallerWithoutTheRole(t *testing.T) {
	v := &fakeVerifier{claims: jwt.Claims{Subject: "user-1"}, roles: []string{"platform:monitor"}}
	h, _ := guarded(v, "platform:admin")

	rec := call(h, "Bearer good")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("guarded route = %d, want 403", rec.Code)
	}
	// Naming the role tells them nothing they could not read in the documentation,
	// and it is the one thing that lets them do something about it.
	if !strings.Contains(rec.Body.String(), "platform:admin") {
		t.Errorf("the refusal does not name the role: %s", rec.Body.String())
	}
}

// A route asking for no particular role still asks the caller to be somebody.
func TestRequireWithNoRolesStillNeedsAValidToken(t *testing.T) {
	v := &fakeVerifier{claims: jwt.Claims{Subject: "user-1"}}
	h, seen := guarded(v)

	if rec := call(h, "Bearer good"); rec.Code != http.StatusOK {
		t.Fatalf("guarded route = %d, want 200", rec.Code)
	}
	if seen.Subject != "user-1" {
		t.Errorf("the handler saw subject %q, want user-1", seen.Subject)
	}
	if rec := call(h, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("with no bearer = %d, want 401", rec.Code)
	}
}

func TestRequireRefusesAMissingOrUnusableToken(t *testing.T) {
	t.Run("no bearer at all", func(t *testing.T) {
		v := &fakeVerifier{claims: jwt.Claims{Subject: "user-1"}}
		h, _ := guarded(v, "platform:admin")

		rec := call(h, "")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("= %d, want 401", rec.Code)
		}
		if rec.Header().Get("WWW-Authenticate") == "" {
			t.Error("no challenge was sent with the 401")
		}
	})

	t.Run("a token the keyset rejects", func(t *testing.T) {
		v := &fakeVerifier{err: errors.New("expired")}
		h, _ := guarded(v, "platform:admin")

		rec := call(h, "Bearer stale")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("= %d, want 401", rec.Code)
		}
		// Answered in one word: the reason a token failed is a hint to whoever is
		// guessing at one.
		if strings.Contains(rec.Body.String(), "expired") {
			t.Errorf("the refusal repeats the keyset's reason: %s", rec.Body.String())
		}
	})
}

// An expired token is a credential for renewing itself at POST /auth/refresh and
// for nothing else. Allowing a window here would make every management route
// accept a token ten minutes after it died.
func TestRequireAllowsNoGraceWindow(t *testing.T) {
	v := &fakeVerifier{claims: jwt.Claims{Subject: "user-1"}, roles: []string{"platform:admin"}}
	h, _ := guarded(v, "platform:admin")

	call(h, "Bearer good")
	if v.window != 0 {
		t.Errorf("the guard asked for %s of grace, want none", v.window)
	}
}

// RFC 6750 says the scheme is case-insensitive, and clients differ.
func TestRequireAcceptsAnyCasingOfBearer(t *testing.T) {
	for _, prefix := range []string{"Bearer ", "bearer ", "BEARER "} {
		v := &fakeVerifier{claims: jwt.Claims{Subject: "user-1"}, roles: []string{"platform:admin"}}
		h, _ := guarded(v, "platform:admin")
		if rec := call(h, prefix+"good"); rec.Code != http.StatusOK {
			t.Errorf("with %q = %d, want 200", prefix, rec.Code)
		}
	}
}

// A handler reached without a guard must not read as an anonymous caller, or a
// wiring mistake would become a silently unattributed write.
func TestFromContextReportsAnUnguardedRequest(t *testing.T) {
	if _, err := FromContext(context.Background()); !errors.Is(err, ErrNoPrincipal) {
		t.Errorf("FromContext() error = %v, want ErrNoPrincipal", err)
	}
}
