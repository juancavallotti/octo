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

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Confine refuses a request for a deployment's pod stats when the deployment
// is in another environment, with the 404 a missing one gets. Pod stats live in
// Redis keyed by deployment and carry no environment of their own, so the
// deployment row is what decides; logs and traces carry theirs and are filtered
// by it in their queries instead.
func Confine(owners Owners, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(segments) >= 2 && segments[0] == "stats" && uuidRe.MatchString(segments[1]) {
			got, found, err := owners.DeploymentEnvironment(r.Context(), segments[1])
			if err != nil {
				slog.ErrorContext(r.Context(), "look up a deployment's environment", "id", segments[1], "error", err)
				httpx.WriteError(w, http.StatusServiceUnavailable, "the request cannot be checked right now")
				return
			}
			if found && got != From(r.Context()) {
				httpx.WriteError(w, http.StatusNotFound, "deployment not found")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
