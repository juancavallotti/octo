package engine

import (
	"strings"
	"testing"
)

// TestEmitKindsUsesTheBlocksOwnVocabulary is what makes the emitter reusable: the
// valid set is the caller's, not a constant baked into the filter.
func TestEmitKindsUsesTheBlocksOwnVocabulary(t *testing.T) {
	known := map[string]bool{"stdout": true, "stderr": true, "exit": true}

	kinds, err := emitKinds("cli-run", known, []string{"stdout", "exit"})
	if err != nil {
		t.Fatalf("emitKinds: %v", err)
	}
	if !kinds["stdout"] || !kinds["exit"] || kinds["stderr"] {
		t.Errorf("emitKinds = %v, want only the named kinds", kinds)
	}

	// An empty list means "everything", not "nothing".
	all, err := emitKinds("cli-run", known, nil)
	if err != nil {
		t.Fatalf("emitKinds(nil): %v", err)
	}
	if all != nil {
		t.Errorf("an empty emit list should pass every kind, got %v", all)
	}
}

// A name outside the vocabulary is an error, not a filter that matches nothing:
// an emit list is how an author says what to watch, so a typo would otherwise
// present as silence.
func TestEmitKindsRejectsAnUnknownKind(t *testing.T) {
	known := map[string]bool{"stdout": true, "stderr": true}
	_, err := emitKinds("cli-run", known, []string{"stduot"})
	if err == nil {
		t.Fatal("an unknown emit kind should fail the build")
	}
	// The error names the block and the whole valid set, so it is actionable
	// without going to the docs.
	for _, want := range []string{"cli-run", "stduot", "stderr, stdout"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

func TestKnownEventKindsIsSorted(t *testing.T) {
	got := knownEventKinds(map[string]bool{"exit": true, "stdout": true, "stderr": true})
	if got != "exit, stderr, stdout" {
		t.Errorf("knownEventKinds = %q, want a sorted list", got)
	}
}
