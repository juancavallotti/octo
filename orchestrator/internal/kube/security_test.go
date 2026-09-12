package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// assertRestricted fails unless every container in pod has given up the default
// capability set.
//
// It sweeps the whole pod rather than naming the containers it knows about,
// which is the entire point of it: a fifth container added to either spec without
// a security context fails here rather than shipping with raw sockets.
func assertRestricted(t *testing.T, what string, pod corev1.PodSpec) {
	t.Helper()
	all := append(append([]corev1.Container{}, pod.InitContainers...), pod.Containers...)
	if len(all) == 0 {
		t.Fatalf("%s: rendered no containers, so this asserts nothing", what)
	}
	for _, ct := range all {
		sc := ct.SecurityContext
		if sc == nil {
			t.Errorf("%s: container %q has no security context, so it keeps NET_RAW", what, ct.Name)
			continue
		}
		if sc.Capabilities == nil || len(sc.Capabilities.Drop) != 1 ||
			sc.Capabilities.Drop[0] != "ALL" {
			t.Errorf("%s: container %q drops %v, want ALL", what, ct.Name, sc.Capabilities)
		}
		if sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
			t.Errorf("%s: container %q may escalate privilege", what, ct.Name)
		}
		if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
			t.Errorf("%s: container %q does not require a non-root user", what, ct.Name)
		}
		// Without the uid, requiring non-root is not a stricter pod — it is a pod
		// that will not start. Every octo image names its user rather than
		// numbering it, and the kubelet refuses what it cannot verify: "image has
		// non-numeric user (nonroot), cannot verify user is non-root". This
		// assertion exists because that failure reached a running cluster.
		if sc.RunAsUser == nil || *sc.RunAsUser != nonrootUID {
			t.Errorf("%s: container %q runs as %v, want the uid %d — a name alone will not start",
				what, ct.Name, sc.RunAsUser, nonrootUID)
		}
	}
}

// A deployed integration is somebody else's code on our pod network, and the
// capability that matters is NET_RAW: with it, a pod can answer for an address
// that is not its own, which is one interception away from serving the
// orchestrator a signing keyset of its choosing.
func TestADeployedIntegrationHoldsNoCapabilities(t *testing.T) {
	c := testClientFor(statsConfig())
	spec := Spec{ID: "1", Name: "app", Slug: "app", Port: 8080}

	pod := c.deployment("octo-dep-1", c.labels(spec), spec).Spec.Template.Spec
	assertRestricted(t, "deployment", pod)
}

// The agentic runner is the one with a shell, so it is the one worth stating
// separately: it holds no more than any other pod does.
func TestTheAgenticRunnerHoldsNoCapabilitiesEither(t *testing.T) {
	c := testClientFor(statsConfig())
	spec := Spec{ID: "1", Name: "app", Slug: "app", Port: 8080, Runner: RunnerAgentic}

	pod := c.deployment("octo-dep-1", c.labels(spec), spec).Spec.Template.Spec
	assertRestricted(t, "agentic deployment", pod)
}

// A dev run is the same code on the same network, started from the editor
// instead of from a deploy — so an exception here would be an exception for
// everything.
func TestADevRunHoldsNoCapabilities(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	assertRestricted(t, "dev run", getDevRunDeployment(t, c, spec.ID).Spec.Template.Spec)
}
