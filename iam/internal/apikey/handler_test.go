package apikey

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/iam/internal/authz"
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			NewHandler(NewService(newFakeRepo())).Register(mux, asCaller(tc.caller))

			req := httptest.NewRequest(http.MethodPost, "/users/user-1/apikeys",
				strings.NewReader(`{"name":"ci","ttlSeconds":3600}`))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
