package main

import (
	"slices"
	"testing"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/devrun"
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

// The sidecar port is where the orchestrator reaches a dev run to reload it, so a
// value that is not a usable port produces a run that starts, reports ready, and then
// cannot be reloaded — a failure with no obvious connection to the setting that caused
// it. Unset is 0, which the kube client reads as its own default.
func TestDevRunSidecarPort(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    int32
		wantErr bool
	}{
		{name: "unset", env: "", want: 0},
		{name: "a port", env: "9000", want: 9000},
		{name: "not a number", env: "eight-thousand", wantErr: true},
		{name: "zero", env: "0", wantErr: true},
		{name: "out of range", env: "70000", wantErr: true},
		{name: "negative", env: "-1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEV_RUN_SIDECAR_PORT", tt.env)
			got, err := devRunSidecarPort()
			if (err != nil) != tt.wantErr {
				t.Fatalf("devRunSidecarPort() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("devRunSidecarPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

// The two likely typos are "60" (which ParseDuration rejects outright) and a
// non-positive value, and both are refused rather than coerced: too short reaps a run
// somebody is using and too long leaves pods up for days, and neither looks like a
// configuration mistake from the outside.
func TestDevRunIdleTimeout(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		want    time.Duration
		wantErr bool
	}{
		{name: "unset takes the default", env: "", want: devrun.DefaultIdleTimeout},
		{name: "a duration", env: "15m", want: 15 * time.Minute},
		{name: "no unit", env: "60", wantErr: true},
		{name: "nonsense", env: "soon", wantErr: true},
		{name: "zero", env: "0s", wantErr: true},
		{name: "negative", env: "-5m", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEV_RUN_IDLE_TIMEOUT", tt.env)
			got, err := devRunIdleTimeout()
			if (err != nil) != tt.wantErr {
				t.Fatalf("devRunIdleTimeout() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("devRunIdleTimeout() = %v, want %v", got, tt.want)
			}
		})
	}
}

// newDevRunService must refuse to build without a hash secret, because the alternative
// is an unkeyed hash: every dev run's public hostname would then be a pure function of
// a user id and an integration id, and that hostname is the only thing guarding a
// publicly reachable dev run.
func TestNewDevRunServiceWithoutACluster(t *testing.T) {
	t.Setenv("DEV_RUN_HASH_SECRET", "")
	svc, err := newDevRunService(nil, nil, nil)
	if err != nil {
		t.Fatalf("no cluster should disable dev runs, not fail: %v", err)
	}
	if svc.Enabled() {
		t.Error("dev runs are enabled with no cluster")
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

// TestAgenticRunnerResources pins the shape the chart emits against the shape the
// orchestrator reads. They are the same shape by construction — the values file
// writes an ordinary requests/limits block and the template `toJson`s it — and
// this is what catches the two ways that stops being true: a rename on either
// side, and a values file whose quantities are numbers rather than strings.
func TestAgenticRunnerResources(t *testing.T) {
	t.Run("unset is no resources", func(t *testing.T) {
		t.Setenv("AGENTIC_RUNNER_RESOURCES", "")
		got, err := agenticRunnerResources()
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(got.Requests) != 0 || len(got.Limits) != 0 {
			t.Errorf("got %+v, want none", got)
		}
	})

	t.Run("the chart's json", func(t *testing.T) {
		t.Setenv("AGENTIC_RUNNER_RESOURCES",
			`{"requests":{"cpu":"200m","memory":"256Mi"},"limits":{"cpu":"1","memory":"1Gi"}}`)
		got, err := agenticRunnerResources()
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if q := got.Requests.Memory().String(); q != "256Mi" {
			t.Errorf("memory request = %s, want 256Mi", q)
		}
		if q := got.Limits.Cpu().String(); q != "1" {
			t.Errorf("cpu limit = %s, want 1", q)
		}
	})

	// Malformed is a startup error rather than a silent none, following
	// INGRESS_ANNOTATIONS: a resources block that failed to parse would otherwise
	// schedule the one workload whose whole purpose is running other programs with
	// no bound on what it may consume.
	// A misspelled key parses cleanly under the default decoder and yields no
	// resources at all — a values file that reads as though it sized the pod and did
	// not. Strict decoding turns that into a startup error naming the field.
	t.Run("a misspelled field is fatal", func(t *testing.T) {
		t.Setenv("AGENTIC_RUNNER_RESOURCES", `{"request":{"cpu":"200m"}}`)
		if _, err := agenticRunnerResources(); err == nil {
			t.Error("accepted \"request\" for \"requests\"")
		}
	})

	t.Run("malformed is fatal", func(t *testing.T) {
		t.Setenv("AGENTIC_RUNNER_RESOURCES", "{not json")
		if _, err := agenticRunnerResources(); err == nil {
			t.Error("accepted malformed JSON")
		}
	})
}
