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
// module — mux.HandleFunc("METHOD /path", …) — and matching that literal is cheaper
// and harder to fool than building the real server, which would need a database.
func TestEveryRegisteredRouteIsDescribed(t *testing.T) {
	registered := registeredRoutes(t)
	if len(registered) == 0 {
		t.Fatal("found no registered routes; the scan is broken, not the spec")
	}

	described := describedRoutes(t)

	var missing []string
	for route := range registered {
		if _, ok := described[route]; !ok {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("these routes are registered but not described:\n  %s\n\n"+
			"annotate the handler, then run `task observability:openapi`",
			strings.Join(missing, "\n  "))
	}
}

// A described route that nothing serves is the other half of the same claim: it
// sends a caller somewhere that answers 404.
func TestEveryDescribedRouteIsRegistered(t *testing.T) {
	registered := registeredRoutes(t)

	var orphaned []string
	for route := range describedRoutes(t) {
		if _, ok := registered[route]; !ok {
			orphaned = append(orphaned, route)
		}
	}
	sort.Strings(orphaned)

	if len(orphaned) > 0 {
		t.Errorf("these routes are described but nothing serves them:\n  %s",
			strings.Join(orphaned, "\n  "))
	}
}

// describedRoutes reads the embedded spec, keyed "METHOD /path".
func describedRoutes(t *testing.T) map[string]struct{} {
	t.Helper()

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(Spec(), &doc); err != nil {
		t.Fatalf("parse embedded spec: %v", err)
	}

	out := make(map[string]struct{}, len(doc.Paths))
	for path, operations := range doc.Paths {
		for method := range operations {
			out[strings.ToUpper(method)+" "+path] = struct{}{}
		}
	}
	return out
}

// handleFunc matches the one form a route is registered in.
var handleFunc = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) ([^"]+)"`)

// registeredRoutes scans the module for route registrations, keyed "METHOD /path".
// The trailing-wildcard marker is dropped so a path matches how it is written in
// the description. Nothing uses one today, but the routes that carry a path value
// are the ones that would.
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

		for _, match := range handleFunc.FindAllStringSubmatch(string(src), -1) {
			out[match[1]+" "+strings.ReplaceAll(match[2], "...", "")] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan module sources: %v", err)
	}
	return out
}
