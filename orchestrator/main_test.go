package main

import (
	"slices"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// The parsing is small, and every case here is one the chart can actually emit:
// an empty list renders an empty string, and a values file with a trailing comma
// or a stray space renders exactly that. A blank surviving the split becomes a
// LocalObjectReference naming a Secret called "", which the kubelet reports as
// missing on every pod the orchestrator deploys — a failure whose message names
// no Secret at all.
func TestImagePullSecretsConfig(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want []string
	}{
		{"unset", "", nil},
		{"one", "regcred", []string{"regcred"}},
		{"several", "regcred,mirror-creds", []string{"regcred", "mirror-creds"}},
		{"spaces", " regcred , mirror-creds ", []string{"regcred", "mirror-creds"}},
		{"trailing comma", "regcred,", []string{"regcred"}},
		{"only separators", " , ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RUNTIME_IMAGE_PULL_SECRETS", tt.env)
			got := imagePullSecretsConfig()
			// Nilness is asserted on its own because slices.Equal(nil, []string{})
			// is true, so the nil cases above would pass against an empty slice —
			// which is the one distinction this function's contract makes.
			if !slices.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("imagePullSecretsConfig() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// The default has to stay Ingress, since that is what every existing install
// runs and none of them set the variable.
func TestKubeConfigDefaultsToIngress(t *testing.T) {
	// kubeConfig reads the process environment, which under `go test` is
	// whatever the developer's shell holds. Cleared explicitly so an inherited
	// ENDPOINT_API cannot change the answer and an inherited malformed
	// INGRESS_ANNOTATIONS cannot fail the test before it asserts anything.
	t.Setenv("ENDPOINT_API", "")
	t.Setenv("INGRESS_ANNOTATIONS", "")

	cfg, err := kubeConfig()
	if err != nil {
		t.Fatalf("kubeConfig: %v", err)
	}
	if cfg.EndpointAPI != kube.EndpointAPIIngress {
		t.Errorf("endpoint API = %q, want %q", cfg.EndpointAPI, kube.EndpointAPIIngress)
	}
}

// Gateway mode publishes routes that carry no annotations at all, so an
// Ingress-only setting left behind in a values file — the ordinary state of a
// release mid-migration — must not be able to stop the process.
func TestKubeConfigIgnoresIngressAnnotationsInGatewayMode(t *testing.T) {
	t.Setenv("INGRESS_ANNOTATIONS", "{not json")
	t.Setenv("ENDPOINT_API", "gateway")
	t.Setenv("BASE_DOMAIN", "apps.example.com")
	t.Setenv("GATEWAY_NAME", "octo-gateway")

	cfg, err := kubeConfig()
	if err != nil {
		t.Fatalf("gateway mode should not parse INGRESS_ANNOTATIONS: %v", err)
	}
	if cfg.ExtraAnnotations != nil {
		t.Errorf("ExtraAnnotations = %v, want nil in gateway mode", cfg.ExtraAnnotations)
	}

	// In ingress mode the same value is still a startup error: there it is a
	// setting that applies, and one that is silently dropped is worse.
	t.Setenv("ENDPOINT_API", "ingress")
	if _, err := kubeConfig(); err == nil {
		t.Error("malformed INGRESS_ANNOTATIONS should be a startup error in ingress mode")
	}
}

func TestKubeConfigGateway(t *testing.T) {
	t.Setenv("ENDPOINT_API", "gateway")
	t.Setenv("BASE_DOMAIN", "apps.example.com")
	t.Setenv("KUBE_NAMESPACE", "octo")
	t.Setenv("GATEWAY_NAME", "octo-gateway")
	t.Setenv("GATEWAY_SECTION_NAME", "https")

	cfg, err := kubeConfig()
	if err != nil {
		t.Fatalf("kubeConfig: %v", err)
	}
	if cfg.EndpointAPI != kube.EndpointAPIGateway {
		t.Errorf("endpoint API = %q, want gateway", cfg.EndpointAPI)
	}
	want := kube.GatewayRef{Name: "octo-gateway", Namespace: "octo", SectionName: "https"}
	if cfg.Gateway != want {
		t.Errorf("gateway = %+v, want %+v", cfg.Gateway, want)
	}

	// A Gateway elsewhere — the usual arrangement, where whoever owns ingress owns
	// the Gateway — overrides the namespace default.
	t.Setenv("GATEWAY_NAMESPACE", "ingress")
	cfg, err = kubeConfig()
	if err != nil {
		t.Fatalf("kubeConfig: %v", err)
	}
	if cfg.Gateway.Namespace != "ingress" {
		t.Errorf("gateway namespace = %q, want ingress", cfg.Gateway.Namespace)
	}
}

// Both of these produce an install that comes up healthy and publishes endpoints
// nothing serves, so both must stop startup instead.
func TestKubeConfigRejectsUnusableSettings(t *testing.T) {
	t.Run("unknown endpoint API", func(t *testing.T) {
		t.Setenv("ENDPOINT_API", "httproute")
		if _, err := kubeConfig(); err == nil {
			t.Error("an unrecognised ENDPOINT_API should be a startup error")
		}
	})
	t.Run("gateway without a name", func(t *testing.T) {
		t.Setenv("ENDPOINT_API", "gateway")
		t.Setenv("BASE_DOMAIN", "apps.example.com")
		if _, err := kubeConfig(); err == nil {
			t.Error("gateway endpoints with no GATEWAY_NAME should be a startup error")
		}
	})
}
