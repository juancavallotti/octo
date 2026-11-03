package apikey

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/iam/internal/authz"
	"github.com/juancavallotti/octo/iam/internal/user"
)

// asCaller stands in for the signed-in guard, putting p on the request the way
// authz.Require would after verifying a token.
func asCaller(p authz.Principal) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(authz.NewContext(r.Context(), p)))
		})
	}
}

// fakeOwners answers every lookup with a developer of that id, or ErrNotFound for
// an id it was told is gone.
type fakeOwners struct{ gone string }

func (f fakeOwners) Get(_ context.Context, id string) (user.User, error) {
	if id == f.gone {
		return user.User{}, user.ErrNotFound
	}
	return developer(id), nil
}

// createBody is a valid create request expiring a day out, carrying roles.
func createBody(roles string) string {
	return `{"name":"ci","roles":` + roles + `,"expiresAt":"` +
		time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `"}`
}

func TestKeysAreManagedOnlyByTheirOwner(t *testing.T) {
	cases := []struct {
		name   string
		caller authz.Principal
		want   int
	}{
		{"the owner", authz.Principal{Subject: "user-1"}, http.StatusCreated},
		{"somebody else", authz.Principal{Subject: "user-2"}, http.StatusForbidden},
		{"an administrator", authz.Principal{Subject: "admin", Roles: []string{"platform:admin"}}, http.StatusForbidden},
		{"the owner's deployment", authz.Principal{Subject: "user-1", Machine: true}, http.StatusForbidden},
		{"the owner, holding a key", authz.Principal{Subject: "user-1", FromAPIKey: true}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			NewHandler(NewService(newFakeRepo()), fakeOwners{}).Register(mux, asCaller(tc.caller))

			req := httptest.NewRequest(http.MethodPost, "/users/user-1/apikeys",
				strings.NewReader(createBody(`["platform:developer"]`)))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

func TestCreateAnswersWithTheReasonAKeyWasRefused(t *testing.T) {
	cases := []struct {
		name  string
		user  string
		body  string
		want  int
		reads string
	}{
		{"scoped", "user-1", createBody(`["platform:developer"]`), http.StatusCreated, `"roles":["platform:developer"]`},
		{"admin on a key", "user-1", createBody(`["platform:admin"]`), http.StatusBadRequest, "cannot be carried by a key"},
		{"no roles", "user-1", createBody(`[]`), http.StatusBadRequest, "at least one role"},
		{"a role the owner lacks", "user-1", createBody(`["platform:operator"]`), http.StatusForbidden, "platform:operator"},
		{"no expiry", "user-1", `{"name":"ci","roles":["platform:developer"]}`, http.StatusBadRequest, "expiration"},
		{"a vanished owner", "gone", createBody(`["platform:developer"]`), http.StatusNotFound, "user not found"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			NewHandler(NewService(newFakeRepo()), fakeOwners{gone: "gone"}).
				Register(mux, asCaller(authz.Principal{Subject: tc.user}))

			req := httptest.NewRequest(http.MethodPost, "/users/"+tc.user+"/apikeys", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.want || !strings.Contains(rec.Body.String(), tc.reads) {
				t.Fatalf("status = %d body %s, want %d containing %q", rec.Code, rec.Body, tc.want, tc.reads)
			}
		})
	}
}
