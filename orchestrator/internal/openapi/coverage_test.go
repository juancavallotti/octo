package openapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// CI regenerates the spec and fails on a diff, which catches an annotation edited
// without regenerating. It does not catch the opposite and more likely mistake: a
// route added with no annotation at all. That regenerates to byte-identical output,
// produces no diff, and passes — leaving the description quietly claiming to
// describe an API it is missing a piece of.
//
// So this reads the routes back out of the source and compares them with the spec.
// It is a coarse test by design: a route is registered in exactly one way in this
// codebase — mux.HandleFunc("METHOD /path", …) — and matching that literal is
// cheaper and harder to fool than building the real server, which would need a
// database and a cluster.
func TestEveryRegisteredRouteIsDescribed(t *testing.T) {
	registered := registeredRoutes(t)
	if len(registered) == 0 {
		t.Fatal("found no registered routes; the scan is broken, not the spec")
	}

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(Spec(), &doc); err != nil {
		t.Fatalf("parse embedded spec: %v", err)
	}

	described := make(map[string]struct{}, len(doc.Paths))
	for path, operations := range doc.Paths {
		for method := range operations {
			described[strings.ToUpper(method)+" "+path] = struct{}{}
		}
	}

	var missing []string
	for route := range registered {
		if _, ok := described[route]; !ok {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("these routes are registered but not described:\n  %s\n\n"+
			"annotate the handler, then run `task orchestrator:openapi`",
			strings.Join(missing, "\n  "))
	}
}

// A described route that nothing serves is the other half of the same claim: it
// sends a caller somewhere that answers 404.
func TestEveryDescribedRouteIsRegistered(t *testing.T) {
	registered := registeredRoutes(t)

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(Spec(), &doc); err != nil {
		t.Fatalf("parse embedded spec: %v", err)
	}

	var orphaned []string
	for path, operations := range doc.Paths {
		for method := range operations {
			route := strings.ToUpper(method) + " " + path
			if _, ok := registered[route]; !ok {
				orphaned = append(orphaned, route)
			}
		}
	}
	sort.Strings(orphaned)

	if len(orphaned) > 0 {
		t.Errorf("these routes are described but nothing serves them:\n  %s",
			strings.Join(orphaned, "\n  "))
	}
}

// handleFunc matches the one form a route is registered in.
var handleFunc = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) ([^"]+)"`)

// kvRouteBase is the single route pattern built from a const rather than written
// inline, so the scan has to substitute it to see the three routes it serves.
const kvRouteBase = "/deployments/{id}/kv/{namespace}/{key...}"

// registeredRoutes scans the module for route registrations, keyed "METHOD /path".
// The trailing-wildcard marker is dropped so a path matches how it is written in
// the description.
func registeredRoutes(t *testing.T) map[string]struct{} {
	t.Helper()

	root := filepath.Join("..", "..")
	out := make(map[string]struct{})

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path) //nolint:gosec // walking this module's own sources
		if err != nil {
			return err
		}
		text := string(src)
		for _, verb := range []string{"GET", "PUT", "DELETE", "POST", "PATCH"} {
			text = strings.ReplaceAll(text, `"`+verb+` "+base`, `"`+verb+` `+kvRouteBase+`"`)
		}

		for _, match := range handleFunc.FindAllStringSubmatch(text, -1) {
			out[match[1]+" "+strings.ReplaceAll(match[2], "...", "")] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan module sources: %v", err)
	}
	return out
}
