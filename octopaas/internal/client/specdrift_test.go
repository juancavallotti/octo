package client

import (
	"os"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// specPath is the public API's description, which lives with the API's flows.
const specPath = "../../../api/static/openapi.yaml"

// The route table must be the spec, operation for operation: an operation the
// spec gained is one the CLI cannot reach yet, and a route the spec lost is one
// the CLI would call and be refused. Each is checked by its operationId and by
// its method and path, so a renamed path is caught as well as a new operation.
func TestRoutesMatchTheSpec(t *testing.T) {

	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read the API's spec: %v", err)
	}
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse the API's spec: %v", err)
	}

	described := map[string]string{}
	for path, item := range spec.Paths {
		for method, op := range item {
			fields, ok := op.(map[string]any)
			if !ok || method == "parameters" {
				continue
			}
			id, _ := fields["operationId"].(string)
			described[id] = strings.ToUpper(method) + " " + path
		}
	}
	if len(described) == 0 {
		t.Fatal("the spec describes no operations; refusing to pass vacuously")
	}

	known := map[string]string{}
	for _, r := range routes {
		if _, dup := known[r.op]; dup {
			t.Errorf("%s is in the route table twice", r.op)
		}
		known[r.op] = r.method + " " + r.path
	}

	var problems []string
	for id, want := range described {
		got, ok := known[id]
		switch {
		case !ok:
			problems = append(problems, "the spec describes "+id+" ("+want+") but the client cannot call it")
		case got != want:
			problems = append(problems, id+" is "+want+" in the spec but "+got+" in the client")
		}
	}
	for id, got := range known {
		if _, ok := described[id]; !ok {
			problems = append(problems, "the client calls "+id+" ("+got+") but the spec no longer describes it")
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}
