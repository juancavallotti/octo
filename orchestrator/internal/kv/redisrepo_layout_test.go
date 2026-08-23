package kv

import (
	"strings"
	"testing"
)

// The runtime compares this module's documented layout against its own (see
// runtime/services/k8s/rediskv_contract_test.go). That comparison is only worth
// anything if the documented layout is what the code actually produces, which is
// what this pins.
func TestKeyOfMatchesTheDocumentedLayout(t *testing.T) {
	want := strings.NewReplacer(
		"{deployment}", "dep-1",
		"{namespace}", "user_volatile",
		"{key}", "profile:7",
	).Replace(volatileKeyLayout)

	if got := keyOf("dep-1", "user_volatile", "profile:7"); got != want {
		t.Errorf("keyOf = %q, but the documented layout renders to %q", got, want)
	}
}
