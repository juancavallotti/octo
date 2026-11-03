package deployment

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// TestRuntimes lists the install's release and every release a deployment runs,
// newest first, once each, and leaves out what cannot be pinned.
func TestRuntimes(t *testing.T) {
	running := func(id, version string) Deployment {
		meta, _ := json.Marshal(Metadata{RuntimeVersion: version})
		return Deployment{ID: id, Metadata: meta}
	}
	repo := &fakeRepo{allRet: []Deployment{
		running("a", "0.11.12"), running("b", "0.11.13"), running("c", "0.11.13"), running("d", "dev"),
	}}
	kc := &fakeKube{status: kube.StatusRunning, runtimeVersion: "0.11.14"}
	svc := NewService(repo, &fakeIntegrations{}, kc)

	current, versions, err := svc.Runtimes(context.Background())
	if err != nil {
		t.Fatalf("Runtimes: %v", err)
	}
	if want := []string{"0.11.14", "0.11.13", "0.11.12"}; current != "0.11.14" || !slices.Equal(versions, want) {
		t.Errorf("Runtimes = %q, %v; want 0.11.14, %v", current, versions, want)
	}
}
