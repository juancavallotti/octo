package openapi

import (
	"encoding/json"
	"testing"
)

// sample is a spec small enough to reason about but shaped like the generated one:
// two tags, a request body wrapped in oneOf (which is what the generator emits), a
// schema that references another, and a schema nothing points at.
const sample = `{
  "openapi": "3.1.0",
  "paths": {
    "/integrations": {
      "get": {
        "tags": ["integrations"],
        "summary": "List integrations",
        "responses": {"200": {"content": {"application/json": {"schema": {"items": {"$ref": "#/components/schemas/Integration"}, "type": "array"}}}}}
      },
      "post": {
        "tags": ["integrations"],
        "requestBody": {"content": {"application/json": {"schema": {"oneOf": [{"type": "object"}, {"$ref": "#/components/schemas/IntegrationRequest"}]}}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Integration"}}}}}
      }
    },
    "/deployments/{id}": {
      "get": {
        "tags": ["deployments"],
        "operationId": "getDeployment",
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Deployment"}}}}}
      }
    }
  },
  "components": {
    "schemas": {
      "Integration": {"type": "object", "properties": {"owner": {"$ref": "#/components/schemas/User"}}},
      "IntegrationRequest": {"type": "object"},
      "Deployment": {"type": "object"},
      "User": {"type": "object"},
      "Unreferenced": {"type": "object"}
    }
  }
}`

// decode parses a filtered document into the two parts every assertion looks at.
func decode(t *testing.T, raw []byte) (paths map[string]any, schemas map[string]any) {
	t.Helper()
	var doc struct {
		Paths      map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal filtered spec: %v", err)
	}
	return doc.Paths, doc.Components.Schemas
}

func TestFilterByTagKeepsOnlyThatTag(t *testing.T) {
	out, err := Filter([]byte(sample), "deployments", "")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	paths, _ := decode(t, out)

	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d: %v", len(paths), paths)
	}
	if _, ok := paths["/deployments/{id}"]; !ok {
		t.Errorf("want /deployments/{id} kept, got %v", paths)
	}
}

// A tag filter has to drop the operations that do not carry the tag, not just the
// paths that have none: keeping a whole path item because one of its methods
// matched hands back the methods that did not.
func TestFilterByTagDropsUntaggedOperationsOnAKeptPath(t *testing.T) {
	const mixed = `{
      "paths": {"/things": {
        "get":  {"tags": ["wanted"]},
        "post": {"tags": ["unwanted"]}
      }},
      "components": {"schemas": {}}
    }`

	out, err := Filter([]byte(mixed), "wanted", "")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	paths, _ := decode(t, out)

	item, ok := paths["/things"].(map[string]any)
	if !ok {
		t.Fatalf("want /things kept, got %v", paths)
	}
	if _, ok := item["get"]; !ok {
		t.Error("want the tagged GET kept")
	}
	if _, ok := item["post"]; ok {
		t.Error("want the untagged POST dropped")
	}
}

func TestFilterByPathIsAPrefixMatch(t *testing.T) {
	out, err := Filter([]byte(sample), "", "/deployments")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	paths, _ := decode(t, out)

	if len(paths) != 1 {
		t.Fatalf("want 1 path, got %d: %v", len(paths), paths)
	}
	if _, ok := paths["/deployments/{id}"]; !ok {
		t.Errorf("want the prefix to match the parameterised route, got %v", paths)
	}
}

// The reason Filter exists: a caller asking about one tag should not be handed
// every schema in the orchestrator.
func TestFilterPrunesUnreachableSchemas(t *testing.T) {
	out, err := Filter([]byte(sample), "deployments", "")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	_, schemas := decode(t, out)

	if _, ok := schemas["Deployment"]; !ok {
		t.Error("want the referenced Deployment kept")
	}
	for _, name := range []string{"Integration", "IntegrationRequest", "User", "Unreferenced"} {
		if _, ok := schemas[name]; ok {
			t.Errorf("want %s pruned, it is unreachable from the deployments tag", name)
		}
	}
}

// Reachability is transitive: Integration references User, and a caller handed
// Integration without User has a document it cannot resolve.
func TestFilterFollowsReferencesThroughSchemas(t *testing.T) {
	out, err := Filter([]byte(sample), "integrations", "")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	_, schemas := decode(t, out)

	for _, name := range []string{"Integration", "IntegrationRequest", "User"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("want %s kept, it is reachable from the integrations tag", name)
		}
	}
	if _, ok := schemas["Unreferenced"]; ok {
		t.Error("want Unreferenced pruned")
	}
}

// The generator wraps a request body in oneOf, so a walker that only looked where a
// schema is "supposed" to be would miss IntegrationRequest entirely.
func TestFilterFindsReferencesInsideOneOf(t *testing.T) {
	out, err := Filter([]byte(sample), "", "/integrations")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	_, schemas := decode(t, out)

	if _, ok := schemas["IntegrationRequest"]; !ok {
		t.Error("want the request body schema found inside oneOf")
	}
}

// Nothing matching is an answer, not a failure — the caller asked a well-formed
// question and the honest response is an empty set of routes.
func TestFilterUnknownTagYieldsNoPathsNotAnError(t *testing.T) {
	out, err := Filter([]byte(sample), "nosuchtag", "")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	paths, schemas := decode(t, out)

	if len(paths) != 0 {
		t.Errorf("want no paths, got %v", paths)
	}
	if len(schemas) != 0 {
		t.Errorf("want no schemas, got %v", schemas)
	}
}

func TestFilterWithoutCriteriaReturnsTheDocumentUnchanged(t *testing.T) {
	out, err := Filter([]byte(sample), "", "")
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if string(out) != sample {
		t.Error("want the input returned byte-for-byte when nothing is filtered")
	}
}

// The embedded artifact has to be a spec, not just valid JSON — a generation step
// that silently produced an empty document would otherwise pass every other test.
func TestEmbeddedSpecDescribesRoutes(t *testing.T) {
	paths, _ := decode(t, Spec())
	if len(paths) == 0 {
		t.Fatal("the embedded spec describes no routes; regenerate with `task orchestrator:openapi`")
	}
	if _, ok := paths["/integrations"]; !ok {
		t.Errorf("want /integrations in the embedded spec, got %d paths", len(paths))
	}
}
