package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

// pinnedRollout rolls a deployment stored with settings onto snap-2, with change's
// runtime, and returns what reached the cluster and the row.
func pinnedRollout(t *testing.T, settings string, runtime *string) (*fakeKube, *fakeRepo, error) {
	t.Helper()
	repo := &fakeRepo{getRet: Deployment{
		ID: "dep-1", IntegrationID: "int-1",
		Settings: json.RawMessage(settings),
		Metadata: json.RawMessage(`{"slug":"orders"}`),
	}}
	snaps := &fakeSnapshots{ret: snapshot.Snapshot{
		ID: "snap-2", IntegrationID: "int-1", Tag: "v2.0", Definition: exposableDef,
	}}
	kc := &fakeKube{status: kube.StatusRunning, runtimeVersion: "0.11.14"}
	svc := NewService(repo, &fakeIntegrations{}, kc, WithSnapshots(snaps))
	_, err := svc.Rollout(context.Background(), "dep-1", RolloutChange{SnapshotID: "snap-2", Runtime: runtime})
	return kc, repo, err
}

func TestRolloutRuntimePin(t *testing.T) {
	pin := func(s string) *string { return &s }
	cases := []struct {
		name     string
		stored   string
		runtime  *string
		wantPin  string
		wantMeta string
	}{
		{"pins to a release", `{"replicas":1}`, pin("0.11.13"), "0.11.13", "0.11.13"},
		{"keeps the pin when not asked", `{"replicas":1,"runtimeVersion":"0.11.13"}`, nil, "0.11.13", "0.11.13"},
		{"latest unpins", `{"replicas":1,"runtimeVersion":"0.11.13"}`, pin("latest"), "", "0.11.14"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kc, repo, err := pinnedRollout(t, tc.stored, tc.runtime)
			if err != nil {
				t.Fatalf("Rollout: %v", err)
			}
			if kc.gotSpec.RuntimePin != tc.wantPin {
				t.Errorf("spec pin = %q, want %q", kc.gotSpec.RuntimePin, tc.wantPin)
			}
			if got := ParseSettings(repo.gotSettings).RuntimeVersion; got != tc.wantPin {
				t.Errorf("persisted pin = %q, want %q", got, tc.wantPin)
			}
			// An unpinned deployment records the install's release, a pinned one
			// its pin.
			if got := ParseMetadata(repo.gotMetadata).RuntimeVersion; got != tc.wantMeta {
				t.Errorf("recorded runtime = %q, want %q", got, tc.wantMeta)
			}
		})
	}
}

// TestRolloutRefusesAnUnsupportedRuntime covers what may not be pinned: something
// that is not a release, one older than the supported list, and one newer than
// the install deploys.
func TestRolloutRefusesAnUnsupportedRuntime(t *testing.T) {
	for _, bad := range []string{"main", "0.11.12", "0.12.0"} {
		kc, _, err := pinnedRollout(t, `{"replicas":1}`, &bad)
		if !errors.Is(err, ErrInvalidRuntimeVersion) {
			t.Fatalf("%s: got %v, want ErrInvalidRuntimeVersion", bad, err)
		}
		if kc.gotSpec.ID != "" {
			t.Errorf("%s: kube.Rollout ran for an unsupported runtime", bad)
		}
	}
}
