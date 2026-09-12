package authz

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPolicyPlacesEachRouteWhereItBelongs(t *testing.T) {
	cases := []struct {
		method, path string
		want         []string
	}{
		// The history, readable by anyone who works here and written by nobody:
		// records arrive over the broker, not over this API.
		{"GET", "/logs", anyone},
		{"GET", "/traces/t1/records/r1", anyone},
		{"GET", "/stats/d1/series", anyone},
		{"POST", "/logs", nil},

		// Saying somebody is looking at an incident is the job of the person
		// watching it; deciding what this installation watches for is not.
		{"POST", "/alerts/incidents/i1/ack", anyone},
		{"GET", "/alerts/watches", anyone},
		{"POST", "/alerts/watches", admins},
		{"DELETE", "/alerts/watches/w1", admins},
		{"POST", "/alerts/preview", admins},

		// How long this installation keeps what it stores, and what that costs.
		{"GET", "/settings/retention", admins},
		{"PUT", "/settings/retention", admins},
		{"GET", "/settings/storage", admins},
		{"POST", "/retention/run", admins},
	}
	for _, tt := range cases {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := required(tt.method, tt.path)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("required = %v, want %v", got, tt.want)
			}
		})
	}
}

// The rule that makes a forgotten route safe: anything unmatched is the
// installation's own business.
func TestAnUnknownRouteIsAdminOnly(t *testing.T) {
	for _, path := range []string{"/something-new", "/ingest", "/"} {
		if got := required("GET", path); len(got) != 1 || got[0] != RoleAdmin {
			t.Errorf("required(GET %s) = %v, want admin only", path, got)
		}
	}
}

// Exemptions are exact, so nothing under an open path is opened with it.
func TestExemptionsAreExact(t *testing.T) {
	for _, path := range []string{"/healthz", "/openapi.json", "/openapi/operations"} {
		if !exempt(path) {
			t.Errorf("%s is not exempt and must be", path)
		}
	}
	for _, path := range []string{"/settings", "/settings/retention", "/healthzz", "/openapi"} {
		if exempt(path) {
			t.Errorf("%s is exempt and must not be", path)
		}
	}
}

// Every route this service registers has to be covered deliberately — by a rule
// or by an exemption. Unmatched routes already fail closed, so this is not about
// safety: it is about a route being admin-only because somebody meant it, rather
// than because they forgot.
func TestEveryRegisteredRouteIsCoveredByARuleOrAnExemption(t *testing.T) {
	handleFunc := regexp.MustCompile(`mux\.HandleFunc\("(GET|PUT|POST|DELETE|PATCH) (/[^"]*)"`)
	root := filepath.Join("..", "..")

	var uncovered []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, readErr := os.ReadFile(path) //nolint:gosec // walking this module's own sources
		if readErr != nil {
			return readErr
		}
		for _, m := range handleFunc.FindAllStringSubmatch(string(src), -1) {
			route := strings.ReplaceAll(m[2], "...", "")
			if exempt(route) || matchedByARule(route) {
				continue
			}
			uncovered = append(uncovered, m[1]+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking sources: %v", err)
	}
	if len(uncovered) > 0 {
		t.Errorf("these routes match no rule and are admin-only by default:\n  %s\n"+
			"Add a rule in policy.go, or an exemption, whichever you meant.",
			strings.Join(uncovered, "\n  "))
	}
}

// matchedByARule reports whether any rule claims this path. Path parameters are
// written {like this}, which stands in for exactly the single segment "*" matches.
func matchedByARule(route string) bool {
	segments := split(route)
	for _, r := range policy {
		if matches(r.pattern, segments) {
			return true
		}
	}
	return false
}
