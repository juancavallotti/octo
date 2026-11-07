package current

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/juancavallotti/octo/observability/internal/httpx"
)

// Owners answers which environment a deployment belongs to.
type Owners interface {
	// DeploymentEnvironment returns the environment ("" for the default) of the
	// deployment with id, and whether there is one.
	DeploymentEnvironment(ctx context.Context, id string) (environment string, found bool, err error)
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// canonicalID returns segment as a canonical uuid when Postgres would read it as
// one — any case, braces or not, hyphens after any group of four digits or none —
// and false when it would not, so no spelling reaches a deployment unchecked.
func canonicalID(segment string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(segment))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	s = strings.ReplaceAll(s, "-", "")
	if !hex32.MatchString(s) {
		return "", false
	}
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32], true
}

// Confine refuses a request for a deployment's pod stats when the deployment
// is in another environment, with the 404 a missing one gets. Pod stats live in
// Redis keyed by deployment and carry no environment of their own, so the
// deployment row is what decides; logs and traces carry theirs and are filtered
// by it in their queries instead. A deployment with no row — deleted, its stats
// not yet expired — is refused too, since nothing else can say whose they are.
func Confine(owners Owners, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		id, isID := "", false
		if len(segments) >= 2 && segments[0] == "stats" {
			id, isID = canonicalID(segments[1])
		}
		if isID {
			got, found, err := owners.DeploymentEnvironment(r.Context(), id)
			if err != nil {
				slog.ErrorContext(r.Context(), "look up a deployment's environment", "id", id, "error", err)
				httpx.WriteError(w, http.StatusServiceUnavailable, "the request cannot be checked right now")
				return
			}
			if !found || got != From(r.Context()) {
				httpx.WriteError(w, http.StatusNotFound, "deployment not found")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
