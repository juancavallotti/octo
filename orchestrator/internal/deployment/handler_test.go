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
		// The digest's own colon is not a tag separator.
		{"octo-runtime@sha256:abcd", ""},
		{"ghcr.io/octo/runtime:0.8.8@sha256:abcd", ""},
	}
	for _, tc := range cases {
		if got := imageTag(tc.image); got != tc.want {
			t.Errorf("imageTag(%q) = %q, want %q", tc.image, got, tc.want)
		}
	}
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
