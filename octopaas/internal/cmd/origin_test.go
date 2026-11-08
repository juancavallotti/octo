package cmd

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/octopaas/internal/config"
)

// A folder pulled from one integration is not pushed onto another, which is what
// the same name is in another environment; the refusal says where it came from.
func TestCheckOrigin(t *testing.T) {

	dir := t.TempDir()
	r := config.Resolved{URL: "http://p", Environment: "stage", EnvironmentSource: config.FromFlag}
	if err := writeOrigin(dir, originOf(r, "id-stage", "billing")); err != nil {
		t.Fatal(err)
	}
	from, err := readOrigin(dir)
	if err != nil {
		t.Fatal(err)
	}
	if from.Environment != "stage" || from.IntegrationID != "id-stage" {
		t.Fatalf("origin = %+v, want stage's billing", from)
	}

	here := config.Resolved{URL: "http://p", EnvironmentSource: config.FromDefault}
	if err := checkOrigin(from, here, "id-stage", "billing"); err != nil {
		t.Errorf("pushing back where it came from: %v", err)
	}
	err = checkOrigin(from, here, "id-default", "billing")
	if err == nil || !strings.Contains(err.Error(), "environment stage") || !strings.Contains(err.Error(), "--environment stage") {
		t.Errorf("pushing onto another environment's billing: err = %v, want a refusal naming stage", err)
	}
}

// A folder nothing was pulled into has no record and goes wherever it is sent.
func TestNoOrigin(t *testing.T) {

	from, err := readOrigin(t.TempDir())
	if err != nil || from != nil {
		t.Fatalf("readOrigin = %v, %v; want nothing", from, err)
	}
	if err := checkOrigin(nil, config.Resolved{}, "any", "billing"); err != nil {
		t.Errorf("a folder with no record: %v", err)
	}
}
