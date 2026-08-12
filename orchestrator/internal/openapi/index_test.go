package openapi

import "testing"

func TestIndexReturnsOneEntryPerMethodAndPath(t *testing.T) {
	got, err := Index([]byte(sample))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 operations (2 on /integrations, 1 on /deployments/{id}), got %d: %v", len(got), got)
	}
}

// Sorted by path then method, so a regeneration cannot reshuffle the output and a
// diff of two indexes reads as a list of route changes.
func TestIndexIsSortedByPathThenMethod(t *testing.T) {
	got, err := Index([]byte(sample))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	want := []struct{ method, path string }{
		{"GET", "/deployments/{id}"},
		{"GET", "/integrations"},
		{"POST", "/integrations"},
	}
	for i, w := range want {
		if got[i].Method != w.method || got[i].Path != w.path {
			t.Errorf("operation %d: want %s %s, got %s %s", i, w.method, w.path, got[i].Method, got[i].Path)
		}
	}
}

func TestIndexCarriesSummaryTagsAndOperationID(t *testing.T) {
	got, err := Index([]byte(sample))
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	byPath := make(map[string]Operation, len(got))
	for _, op := range got {
		byPath[op.Method+" "+op.Path] = op
	}

	list := byPath["GET /integrations"]
	if list.Summary != "List integrations" {
		t.Errorf("want the summary carried, got %q", list.Summary)
	}
	if len(list.Tags) != 1 || list.Tags[0] != "integrations" {
		t.Errorf("want the tag carried (it is what a caller filters on next), got %v", list.Tags)
	}

	if id := byPath["GET /deployments/{id}"].OperationID; id != "getDeployment" {
		t.Errorf("want the operationId carried, got %q", id)
	}
}

// The index is the entry point for a caller that cannot afford the whole document,
// so it has to describe the real API rather than the test fixture.
func TestIndexOfTheEmbeddedSpecIsNotEmpty(t *testing.T) {
	got, err := Index(Spec())
	if err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("the embedded spec indexes no operations; regenerate with `task orchestrator:openapi`")
	}
}

func TestIndexRejectsGarbage(t *testing.T) {
	if _, err := Index([]byte("not json")); err == nil {
		t.Fatal("want an error for a spec that does not parse")
	}
}
