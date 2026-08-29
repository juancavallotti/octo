package deployment

import (
	"encoding/json"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// TestImageTag covers the tag split used to label a deployment with the runtime it
// is running: a plain tag, a registry with a port (whose colon is not a tag), and
// the references that have no tag to show.
func TestImageTag(t *testing.T) {
	cases := []struct {
		image string
		want  string
	}{
		{"octo-runtime:v0.8.8", "v0.8.8"},
		{"ghcr.io/juancavallotti/octo-runtime:0.8.8", "0.8.8"},
		{"registry:5000/octo/runtime:dev", "dev"},
		{"registry:5000/octo/runtime", ""},
		{"octo-runtime", ""},
		{"", ""},
		// The digest's own colon is not a tag separator, in either spelling: a
		// deploy pinned by digest gives the first, and the kubelet reports that
		// pod's image as the second.
		{"octo-runtime@sha256:abcd", ""},
		{"ghcr.io/octo/runtime:0.8.8@sha256:abcd", ""},
		{"sha256:ed08b693c518be5d6995e2e2edd6bb8ab42972a34a2c375cd7e7a85aecf8e210", ""},
		{"sha512:ed08b693", ""},
	}
	for _, tc := range cases {
		if got := imageTag(tc.image); got != tc.want {
			t.Errorf("imageTag(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
}

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
		})
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
		})
		if got.RuntimeVersion != "0.9.0" {
			t.Errorf("RuntimeVersion = %q, want the recorded 0.9.0", got.RuntimeVersion)
		}
	})

	t.Run("a deployment predating both says nothing", func(t *testing.T) {
		got := toResponse(Deployment{
			Metadata: meta(Metadata{}),
			Detail:   kube.Status{RuntimeImage: "sha256:abcd"},
		})
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
		})
		if got.RuntimeImage != "octo-runtime:v1" || got.RuntimeVersion != "v1" {
			t.Errorf("runtime = %q/%q, want octo-runtime:v1/v1", got.RuntimeImage, got.RuntimeVersion)
		}
	})

	t.Run("falls back to the recorded image", func(t *testing.T) {
		got := toResponse(Deployment{Metadata: withMeta("octo-runtime:v2")})
		if got.RuntimeImage != "octo-runtime:v2" || got.RuntimeVersion != "v2" {
			t.Errorf("runtime = %q/%q, want octo-runtime:v2/v2", got.RuntimeImage, got.RuntimeVersion)
		}
	})

	t.Run("empty when neither knows", func(t *testing.T) {
		got := toResponse(Deployment{Metadata: withMeta("")})
		if got.RuntimeImage != "" || got.RuntimeVersion != "" {
			t.Errorf("runtime = %q/%q, want empty", got.RuntimeImage, got.RuntimeVersion)
		}
	})
}
