package deployment

import (
	"encoding/json"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// TestToResponseRuntimeVersion covers where the reported version comes from when
// the image reference cannot supply one — which is every install that pins the
// runtime by digest, and is the case the recorded version exists for.
func TestToResponseRuntimeVersion(t *testing.T) {
	meta := func(m Metadata) json.RawMessage {
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		return raw
	}
	const digest = "reg.example/octo-runtime@sha256:abcd"

	t.Run("a tag on the running image wins", func(t *testing.T) {
		got := toResponse(Deployment{
			Metadata: meta(Metadata{RuntimeImage: digest, RuntimeVersion: "0.9.0"}),
			Detail:   kube.Status{RuntimeImage: "reg.example/octo-runtime:0.8.8"},
		}, "")
		if got.RuntimeVersion != "0.8.8" {
			t.Errorf("RuntimeVersion = %q, want the running 0.8.8", got.RuntimeVersion)
		}
	})

	// The whole point: a digest-pinned deployment still says which octo it is.
	t.Run("the recorded version carries a digest-pinned deployment", func(t *testing.T) {
		got := toResponse(Deployment{
			Metadata: meta(Metadata{RuntimeImage: digest, RuntimeVersion: "0.9.0"}),
			// The bare digest the kubelet reports for such a pod.
			Detail: kube.Status{RuntimeImage: "sha256:abcd"},
		}, "")
		if got.RuntimeVersion != "0.9.0" {
			t.Errorf("RuntimeVersion = %q, want the recorded 0.9.0", got.RuntimeVersion)
		}
	})

	t.Run("a deployment predating both says nothing", func(t *testing.T) {
		got := toResponse(Deployment{
			Metadata: meta(Metadata{}),
			Detail:   kube.Status{RuntimeImage: "sha256:abcd"},
		}, "")
		if got.RuntimeVersion != "" {
			t.Errorf("RuntimeVersion = %q, want empty", got.RuntimeVersion)
		}
	})
}

// TestToResponseRuntimeImage covers which runtime a deployment is reported to be
// on: the live one the pods report, the recorded one when the cluster has nothing
// to say, and nothing at all for a deployment predating either.
func TestToResponseRuntimeImage(t *testing.T) {
	withMeta := func(image string) json.RawMessage {
		raw, err := json.Marshal(Metadata{Name: "orders", RuntimeImage: image})
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		return raw
	}

	t.Run("live image wins over the recorded one", func(t *testing.T) {
		got := toResponse(Deployment{
			Metadata: withMeta("octo-runtime:v2"),
			Detail:   kube.Status{RuntimeImage: "octo-runtime:v1"},
		}, "")
		if got.RuntimeImage != "octo-runtime:v1" || got.RuntimeVersion != "v1" {
			t.Errorf("runtime = %q/%q, want octo-runtime:v1/v1", got.RuntimeImage, got.RuntimeVersion)
		}
	})

	t.Run("falls back to the recorded image", func(t *testing.T) {
		got := toResponse(Deployment{Metadata: withMeta("octo-runtime:v2")}, "")
		if got.RuntimeImage != "octo-runtime:v2" || got.RuntimeVersion != "v2" {
			t.Errorf("runtime = %q/%q, want octo-runtime:v2/v2", got.RuntimeImage, got.RuntimeVersion)
		}
	})

	t.Run("empty when neither knows", func(t *testing.T) {
		got := toResponse(Deployment{Metadata: withMeta("")}, "")
		if got.RuntimeImage != "" || got.RuntimeVersion != "" {
			t.Errorf("runtime = %q/%q, want empty", got.RuntimeImage, got.RuntimeVersion)
		}
	})
}

// TestToResponseUpgradeTo covers when a deployment is offered the install's
// runtime: only when it is strictly behind it.
func TestToResponseUpgradeTo(t *testing.T) {
	running := func(image string) Deployment {
		return Deployment{Detail: kube.Status{RuntimeImage: image}}
	}
	cases := []struct {
		name    string
		d       Deployment
		current string
		want    string
	}{
		{"behind", running("octo-runtime:0.11.13"), "0.11.14", "0.11.14"},
		{"current", running("octo-runtime:0.11.14"), "0.11.14", ""},
		{"ahead", running("octo-runtime:0.12.0"), "0.11.14", ""},
		{"unknown install runtime", running("octo-runtime:0.11.13"), "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := toResponse(tc.d, tc.current).UpgradeTo; got != tc.want {
				t.Errorf("UpgradeTo = %q, want %q", got, tc.want)
			}
		})
	}
}
