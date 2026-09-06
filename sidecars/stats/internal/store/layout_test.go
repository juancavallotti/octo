package store

import (
	"strings"
	"testing"
	"time"
)

// The layout is read from outside this module — by whatever eventually consumes
// these stats, and by anyone debugging with redis-cli — so a silent change to it
// is a silent break. Asserted as a literal for the same reason
// runtime/services/k8s/rediskv_contract_test.go asserts volatileKeyLayout: the
// test is the contract, and changing it has to be a deliberate edit.
func TestLayoutIsStable(t *testing.T) {
	const want = "octo:stats:v0:{deployment}:{pod}:{tier}"
	if Layout != want {
		t.Errorf("Layout = %q, want %q", Layout, want)
	}
}

// Every key a store writes has to match the layout it publishes, or the
// documentation and the behaviour have drifted apart.
func TestKeysMatchLayout(t *testing.T) {
	s := New(nil, Config{DeploymentID: "dep-1", PodName: "octo-dep-1-abc"})

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"pods index", PodsKey("dep-1"), "octo:stats:v0:dep-1:pods"},
		{"meta", s.podKey(metaKey), "octo:stats:v0:dep-1:octo-dep-1-abc:meta"},
		{"live", s.podKey(liveKey), "octo:stats:v0:dep-1:octo-dep-1-abc:live"},
		{"rollup", s.podKey(rollupKey), "octo:stats:v0:dep-1:octo-dep-1-abc:rollup"},
		{"dictionary", s.podKey(dictKey, "3"), "octo:stats:v0:dep-1:octo-dep-1-abc:dict:3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("key = %q, want %q", tc.got, tc.want)
			}
			if !strings.HasPrefix(tc.got, prefix+":") {
				t.Errorf("key %q does not start with the versioned prefix", tc.got)
			}
		})
	}
}

// The deployment id comes before the pod name, which is the whole reason a
// reader can go from one deployment to all of its pods.
func TestDeploymentSegmentPrecedesPod(t *testing.T) {
	s := New(nil, Config{DeploymentID: "dep-1", PodName: "pod-1"})
	key := s.podKey(liveKey)
	dep, pod := strings.Index(key, "dep-1"), strings.Index(key, "pod-1")
	if dep < 0 || pod < 0 || dep > pod {
		t.Errorf("key %q does not put the deployment before the pod", key)
	}
	if !strings.HasPrefix(key, PodsKey("dep-1")[:len(PodsKey("dep-1"))-len(":pods")]) {
		t.Errorf("key %q does not share a prefix with the pod index", key)
	}
}

func TestTTLs(t *testing.T) {
	s := New(nil, Config{RollupInterval: time.Hour, Retention: 7 * 24 * time.Hour})

	// The live tier outlives one bucket, so a pod that stops exactly on a
	// boundary does not lose the bucket it was in before anything can read it.
	if got, want := s.liveTTL(), 2*time.Hour; got != want {
		t.Errorf("liveTTL = %v, want %v", got, want)
	}
	// Everything else outlives the retention window, so the oldest row does not
	// expire at the instant it becomes the oldest row.
	if got := s.rollupTTL(); got <= 7*24*time.Hour {
		t.Errorf("rollupTTL = %v, want more than the %v retention", got, 7*24*time.Hour)
	}
}
