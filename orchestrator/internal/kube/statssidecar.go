package kube

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// statsSidecarName is the container name, and how it appears in
	// `kubectl logs -c`.
	statsSidecarName = "stats"
	// statsPortName names the container port its probes and status page answer on.
	statsPortName = "stats"
	// defaultStatsSidecarPort matches the sidecar's own default
	// (sidecars/stats/config.go). Deliberately not the dev sidecar's 8099: the two
	// never share a pod, but a port that says which sidecar answered is worth more
	// than a number reused out of habit.
	defaultStatsSidecarPort int32 = 8098

	// statsHealthPath is the sidecar's liveness endpoint. There is no readiness
	// path here on purpose — see statsSidecarContainer.
	statsHealthPath = "/healthz"

	// Liveness timings. Rare and forgiving, because all this probe can do is
	// restart a container whose only job is bookkeeping. The sidecar has no
	// startup work to wait for, so there is no startup probe either.
	statsLivenessPeriodSeconds   = 30
	statsLivenessFailureThresh   = 3
	statsLivenessInitialDelaySec = 10

	// Environment the sidecar reads; see sidecars/stats/config.go.
	//
	// DEPLOYMENT_ID rather than the runtime's OCTO_DEPLOYMENT_ID: the sidecar is
	// not an octo runtime and does not read the runtime's environment contract,
	// so it takes the plain name. The value is the same deployment id either way,
	// and it is the first segment of every key the sidecar writes.
	envStatsDeploymentID   = "DEPLOYMENT_ID"
	envStatsSampleInterval = "STATS_SAMPLE_INTERVAL"
	envStatsRollupInterval = "STATS_ROLLUP_INTERVAL"
	envStatsRetention      = "STATS_RETENTION"
	envPort                = "PORT"
)

// StatsSidecar is the orchestrator's configuration for the pod stats sidecar.
// Zero value means the feature is off and no pod gains a container.
type StatsSidecar struct {
	Image string
	Port  int32

	SampleInterval time.Duration
	RollupInterval time.Duration
	Retention      time.Duration
}

// enabled reports whether deployments should carry the sidecar.
//
// The image alone is not enough: the sidecar's whole output goes to Redis, so on
// an install without one it would start, fail every write, and log about it
// forever. Requiring both means "no Redis" reads as "feature off" rather than as
// a broken container in every production pod.
func (c *Client) statsSidecarEnabled() bool {
	return c.statsSidecar.Image != "" && redisEnv(c.runtimeServices) != nil
}

// statsSidecarContainer builds the sidecar as a native sidecar: an init
// container with RestartPolicy Always, the same shape devrun.go uses.
//
// Native rather than an ordinary container for the ordering at the END of a
// pod's life. Kubernetes terminates restartable init containers after the app
// containers, so the sidecar is still running when the runtime stops and the
// bucket it flushes on the way out is complete rather than truncated. Nothing
// about the start ordering matters here, which is why there is no startup probe
// — unlike the dev sidecar, whose whole purpose is to populate a directory
// before the runtime looks at it.
//
// # No readiness probe
//
// This is the load-bearing omission, and it is deliberate rather than an
// oversight. Kubernetes folds a restartable init container's readiness into the
// POD's readiness. A readiness probe here would therefore mean that whenever the
// stats sidecar was unhappy — a Redis outage, most obviously — every integration
// pod in the namespace would leave its Service endpoints at once, and production
// traffic would stop in order to protect the collection of statistics.
//
// The sidecar's own probes answer 200 unconditionally for the same reason
// (sidecars/stats/internal/api). Both halves are needed: this one so the trade
// cannot be made by configuration, that one so it cannot be made by accident.
//
// # No resources
//
// Consistent with containerResources: no integration pod has ever carried
// requests or limits, and adding them for every deployment at once would
// reschedule an entire installation.
func (c *Client) statsSidecarContainer(spec Spec) corev1.Container {
	port := c.statsSidecar.Port
	if port < 1 {
		port = defaultStatsSidecarPort
	}
	always := corev1.ContainerRestartPolicyAlways

	return corev1.Container{
		Name:            statsSidecarName,
		Image:           c.statsSidecar.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		RestartPolicy:   &always,
		Env:             c.statsSidecarEnv(spec, port),
		Ports: []corev1.ContainerPort{{
			Name:          statsPortName,
			ContainerPort: port,
		}},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: statsHealthPath,
					Port: intstr.FromString(statsPortName),
				},
			},
			InitialDelaySeconds: statsLivenessInitialDelaySec,
			PeriodSeconds:       statsLivenessPeriodSeconds,
			TimeoutSeconds:      probeTimeoutSeconds,
			FailureThreshold:    statsLivenessFailureThresh,
		},
	}
}

// statsSidecarEnv is the sidecar's environment; see sidecars/stats/config.go for
// what each value means.
//
// It carries no token and no credential. The sidecar speaks to exactly two
// peers — the runtime on the pod's own loopback, and Redis — and reaches neither
// through anything it has to authenticate to. The pod keeps running on the
// namespace default ServiceAccount with no cluster access, the same invariant a
// dev run keeps.
//
// The durations are emitted only when set, so an unconfigured installation gets
// the sidecar's own defaults rather than a zero it would reject at startup.
func (c *Client) statsSidecarEnv(spec Spec, port int32) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: envPort, Value: fmt.Sprintf("%d", port)},
		{Name: envRuntimeAdmin, Value: fmt.Sprintf("127.0.0.1:%d", adminPort)},
		{Name: envStatsDeploymentID, Value: spec.ID},
		// The pod name comes from the downward API rather than being derived: a
		// Deployment's pods are named by the ReplicaSet controller, so this is the
		// only place the real name exists.
		{Name: envPodName, ValueFrom: fieldRef("metadata.name")},
	}
	// Guaranteed non-nil by statsSidecarEnabled, which is the only caller's gate.
	if redis := redisEnv(c.runtimeServices); redis != nil {
		env = append(env, *redis)
	}

	for _, d := range []struct {
		name  string
		value time.Duration
	}{
		{envStatsSampleInterval, c.statsSidecar.SampleInterval},
		{envStatsRollupInterval, c.statsSidecar.RollupInterval},
		{envStatsRetention, c.statsSidecar.Retention},
	} {
		if d.value > 0 {
			env = append(env, corev1.EnvVar{Name: d.name, Value: d.value.String()})
		}
	}
	return env
}
