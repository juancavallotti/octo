package current

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// Owners answers which environment a row belongs to.
type Owners interface {
	// EnvironmentOf returns the environment ("" for the default) of the row of
	// kind ("integrations", "deployments", "snapshots" or "folders") with id, and
	// whether there is one.
	EnvironmentOf(ctx context.Context, kind, id string) (environment string, found bool, err error)
}

// owned are the path shapes that name a row: the collection, then the id. A
// path naming two (a folder and an integration in it) is checked for both.
var owned = map[string]bool{
	"integrations": true,
	"deployments":  true,
	"snapshots":    true,
	"folders":      true,
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// canonicalID returns segment as a canonical uuid when Postgres would read it as
// one, and false when it would not.
//
// Postgres takes a uuid in any case, with or without braces, and with hyphens
// after any group of four digits or none at all. A row is checked under its
// canonical form, so a spelling that only Postgres recognises cannot address a
// row this guard never looked at.
func canonicalID(segment string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(segment))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
	s = strings.ReplaceAll(s, "-", "")
	if !hex32.MatchString(s) {
		return "", false
	}
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32], true
}

// Confine refuses a request that names a row in another environment, with the
// same 404 the handler gives a row that does not exist: a caller in staging
// learns nothing about what exists in production, not even that an id is real.
//
// It is the one place by-id access is scoped. Every route beneath an
// integration, a deployment, a snapshot or a folder — files, resources,
// versions, test runs, KV, objects, agent memory, logs — is addressed through
// one of them, so checking the owner here confines all of them without each
// handler having to remember.
//
// A row that does not exist is passed through for the handler to report, and
// so is a segment that is not an id (POST /integrations/bundle, PUT
// /folders/reorder). A deployment is the exception: the stores beneath it — KV,
// objects, agent memory — are keyed by its id alone and outlive it until they are
// cleaned up, so once its row is gone nothing can say whose they are, and it is
// refused like one in another environment.
func Confine(owners Owners, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		environment := From(r.Context())
		for _, ref := range ownedRefs(r.URL.Path) {
			got, found, err := owners.EnvironmentOf(r.Context(), ref.kind, ref.id)
			if err != nil {
				slog.ErrorContext(r.Context(), "look up a row's environment", "kind", ref.kind, "id", ref.id, "error", err)
				httpx.WriteError(w, http.StatusServiceUnavailable, "the request cannot be checked right now")
				return
			}
			if (found && got != environment) || (!found && ref.kind == "deployments") {
				httpx.WriteError(w, http.StatusNotFound, strings.TrimSuffix(ref.kind, "s")+" not found")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type ref struct{ kind, id string }

// ownedRefs returns every (collection, id) pair the path names.
func ownedRefs(path string) []ref {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	var out []ref
	for i := 0; i+1 < len(segments); i++ {
		if !owned[segments[i]] {
			continue
		}
		if id, ok := canonicalID(segments[i+1]); ok {
			out = append(out, ref{kind: segments[i], id: id})
			i++
		}
	}
	return out
}
