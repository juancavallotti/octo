package kube

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// statsConfig is testConfig plus a stats sidecar and the Redis it needs.
func statsConfig() Config {
	cfg := testConfig("octo.example.com")
	cfg.StatsSidecar = StatsSidecar{Image: "octo-statssidecar:dev"}
	cfg.RuntimeServices = RuntimeServices{
		Module:   "k8s",
		RedisURL: "redis://octo-redis:6379",
	}
	return cfg
}

// statsContainer returns the injected sidecar from a rendered Deployment, or
// fails when it is absent.
func statsContainer(t *testing.T, c *Client, spec Spec) corev1.Container {
	t.Helper()
	pod := c.deployment("octo-dep-1", c.labels(spec), spec).Spec.Template.Spec
	for _, ct := range pod.InitContainers {
		if ct.Name == statsSidecarName {
			return ct
		}
	}
	t.Fatal("stats sidecar not injected")
	return corev1.Container{}
}

// envOf reads one variable's literal value from a container.
func statsEnvOf(ct corev1.Container, name string) (string, bool) {
	for _, e := range ct.Env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

// The load-bearing assertion of this whole feature.
//
// Kubernetes folds a restartable init container's readiness into the POD's
// readiness, so a readiness probe here would let a Redis outage pull every
// integration in the namespace out of its Service. Observability must never be
// able to stop the traffic it observes.
func TestStatsSidecarHasNoReadinessProbe(t *testing.T) {
	c := testClientFor(statsConfig())
	ct := statsContainer(t, c, Spec{ID: "1", Port: 8080})

	if ct.ReadinessProbe != nil {
		t.Error("stats sidecar has a readiness probe; a restartable init container's " +
			"readiness gates the pod's, so this would let a cache outage stop traffic")
	}
	if ct.StartupProbe != nil {
		t.Error("stats sidecar has a startup probe; nothing waits on it, and a " +
			"failing one would block the runtime container from starting")
	}
	// Liveness is fine: all it can do is restart a container that does bookkeeping.
	if ct.LivenessProbe == nil {
		t.Error("stats sidecar has no liveness probe; a wedged one would never recover")
	}
}

// A native sidecar, so it terminates after the runtime and its final flush
// covers a complete bucket.
func TestStatsSidecarIsANativeSidecar(t *testing.T) {
	c := testClientFor(statsConfig())
	ct := statsContainer(t, c, Spec{ID: "1"})

	if ct.RestartPolicy == nil || *ct.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Fatalf("restart policy = %v, want Always (a native sidecar)", ct.RestartPolicy)
	}
}

// Off by default, and off in a way that changes nothing: a deployment on an
// installation without the feature must render exactly the spec it renders
// today, or turning nothing on would roll every pod.
func TestStatsSidecarOffByDefault(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "no image configured",
			cfg:  testConfig("octo.example.com"),
		},
		{
			// The sidecar's entire output goes to Redis. Without one it would
			// start, fail every write and log about it forever, so "no Redis"
			// has to read as "feature off".
			name: "image but no redis",
			cfg: func() Config {
				cfg := testConfig("octo.example.com")
				cfg.StatsSidecar = StatsSidecar{Image: "octo-statssidecar:dev"}
				return cfg
			}(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := testClientFor(tc.cfg)
			spec := Spec{ID: "1", Port: 8080}
			pod := c.deployment("octo-dep-1", c.labels(spec), spec).Spec.Template.Spec

			if pod.InitContainers != nil {
				t.Errorf("InitContainers = %v, want nil", pod.InitContainers)
			}
			for _, e := range pod.Containers[0].Env {
				if e.Name == envMetrics {
					t.Error("OCTO_METRICS is set with no sidecar to scrape it")
				}
			}
		})
	}
}

// The runtime does not serve /metrics unless asked, so injecting the sidecar has
// to turn it on or there is nothing to scrape.
func TestStatsSidecarEnablesRuntimeMetrics(t *testing.T) {
	c := testClientFor(statsConfig())
	spec := Spec{ID: "1", Port: 8080}
	runtime := c.deployment("octo-dep-1", c.labels(spec), spec).Spec.Template.Spec.Containers[0]

	got, ok := statsEnvOf(runtime, envMetrics)
	if !ok || got != "true" {
		t.Errorf("%s = %q (present %v), want \"true\"", envMetrics, got, ok)
	}
}

// An integration that declared OCTO_METRICS among its own env must not be able
// to turn the endpoint back off, because the sidecar beside it depends on it.
// Kubernetes resolves a duplicate name to the last entry, so this checks order.
func TestStatsSidecarMetricsWinsOverIntegrationEnv(t *testing.T) {
	c := testClientFor(statsConfig())
	spec := Spec{ID: "1", Port: 8080, Env: map[string]string{envMetrics: "false"}}
	runtime := c.deployment("octo-dep-1", c.labels(spec), spec).Spec.Template.Spec.Containers[0]

	last := ""
	for _, e := range runtime.Env {
		if e.Name == envMetrics {
			last = e.Value
		}
	}
	if last != "true" {
		t.Errorf("last %s entry = %q, want \"true\" so the switch wins", envMetrics, last)
	}
}

func TestStatsSidecarEnv(t *testing.T) {
	cfg := statsConfig()
	cfg.StatsSidecar.SampleInterval = time.Second
	cfg.StatsSidecar.RollupInterval = 15 * time.Minute
	cfg.StatsSidecar.Retention = 7 * 24 * time.Hour
	c := testClientFor(cfg)
	ct := statsContainer(t, c, Spec{ID: "dep-42"})

	tests := []struct {
		name string
		want string
	}{
		// The first segment of every key the sidecar writes.
		{envStatsDeploymentID, "dep-42"},
		{envRuntimeAdmin, "127.0.0.1:39999"},
		{envRedisURL, "redis://octo-redis:6379"},
		{envStatsSampleInterval, "1s"},
		{envStatsRollupInterval, "15m0s"},
		{envStatsRetention, "168h0m0s"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := statsEnvOf(ct, tc.name)
			if !ok {
				t.Fatalf("%s not set", tc.name)
			}
			if got != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
			}
		})
	}

	// The pod name has to come from the downward API: a Deployment's pods are
	// named by the ReplicaSet controller, so nothing here knows it.
	var podName *corev1.EnvVar
	for i, e := range ct.Env {
		if e.Name == envPodName {
			podName = &ct.Env[i]
		}
	}
	if podName == nil || podName.ValueFrom == nil || podName.ValueFrom.FieldRef == nil {
		t.Fatalf("%s = %+v, want a downward-API field reference", envPodName, podName)
	}
	if got := podName.ValueFrom.FieldRef.FieldPath; got != "metadata.name" {
		t.Errorf("%s field path = %q, want metadata.name", envPodName, got)
	}
}

// Unset durations are omitted rather than sent as zero, so one place owns each
// default and it is the binary — which rejects a zero at startup.
func TestStatsSidecarOmitsUnsetDurations(t *testing.T) {
	c := testClientFor(statsConfig())
	ct := statsContainer(t, c, Spec{ID: "1"})

	for _, name := range []string{envStatsSampleInterval, envStatsRollupInterval, envStatsRetention} {
		if v, ok := statsEnvOf(ct, name); ok {
			t.Errorf("%s = %q, want it omitted so the sidecar's own default applies", name, v)
		}
	}
}

// A managed Redis URL carries a password, and a literal in the rendered
// Deployment is readable by anyone who can read workloads. The sidecar takes the
// reference by the same rule the runtime container does.
func TestStatsSidecarTakesRedisBySecretReference(t *testing.T) {
	cfg := statsConfig()
	cfg.RuntimeServices.RedisURL = ""
	cfg.RuntimeServices.RedisSecret = SecretKeyRef{Name: "octo-redis", Key: "redis-url"}
	c := testClientFor(cfg)
	ct := statsContainer(t, c, Spec{ID: "1"})

	for _, e := range ct.Env {
		if e.Name != envRedisURL {
			continue
		}
		if e.Value != "" {
			t.Errorf("%s has a literal value %q, want a secret reference", envRedisURL, e.Value)
		}
		if e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
			t.Fatalf("%s = %+v, want a secret reference", envRedisURL, e)
		}
		if got := e.ValueFrom.SecretKeyRef.Name; got != "octo-redis" {
			t.Errorf("secret name = %q, want octo-redis", got)
		}
		return
	}
	t.Fatalf("%s not set", envRedisURL)
}

func TestStatsSidecarPort(t *testing.T) {
	t.Run("defaults to the sidecar's own", func(t *testing.T) {
		c := testClientFor(statsConfig())
		ct := statsContainer(t, c, Spec{ID: "1"})
		if got := ct.Ports[0].ContainerPort; got != defaultStatsSidecarPort {
			t.Errorf("port = %d, want %d", got, defaultStatsSidecarPort)
		}
		if got, _ := statsEnvOf(ct, envPort); got != "8098" {
			t.Errorf("PORT = %q, want 8098 to match the declared port", got)
		}
	})

	t.Run("configured port reaches both the container and its env", func(t *testing.T) {
		cfg := statsConfig()
		cfg.StatsSidecar.Port = 9100
		c := testClientFor(cfg)
		ct := statsContainer(t, c, Spec{ID: "1"})

		if got := ct.Ports[0].ContainerPort; got != 9100 {
			t.Errorf("container port = %d, want 9100", got)
		}
		if got, _ := statsEnvOf(ct, envPort); got != "9100" {
			t.Errorf("PORT = %q, want 9100", got)
		}
	})
}

// The sidecar carries no credential and the pod gains no cluster access, which
// is the same invariant a dev run keeps.
func TestStatsSidecarCarriesNoCredential(t *testing.T) {
	c := testClientFor(statsConfig())
	spec := Spec{ID: "1"}
	pod := c.deployment("octo-dep-1", c.labels(spec), spec).Spec.Template.Spec
	ct := statsContainer(t, c, spec)

	if pod.ServiceAccountName != "" {
		t.Errorf("ServiceAccountName = %q, want none", pod.ServiceAccountName)
	}
	for _, e := range ct.Env {
		switch e.Name {
		case "ORCHESTRATOR_URL", "SIDECAR_TOKEN", "DEV_RUN_TOKEN":
			t.Errorf("stats sidecar carries %s; it speaks only to loopback and Redis", e.Name)
		}
	}
	if len(ct.VolumeMounts) != 0 {
		t.Errorf("VolumeMounts = %v, want none; it writes no files", ct.VolumeMounts)
	}
}
