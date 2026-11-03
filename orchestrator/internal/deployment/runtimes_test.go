package deployment

import (
	"slices"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// TestRuntimes offers the install's release and the supported ones under it.
func TestRuntimes(t *testing.T) {
	kc := &fakeKube{status: kube.StatusRunning, runtimeVersion: "0.11.14"}
	svc := NewService(&fakeRepo{}, &fakeIntegrations{}, kc)

	current, versions := svc.Runtimes()
	if want := []string{"0.11.14", "0.11.13"}; current != "0.11.14" || !slices.Equal(versions, want) {
		t.Errorf("Runtimes = %q, %v; want 0.11.14, %v", current, versions, want)
	}
}
