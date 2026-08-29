package kube

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// agenticConfig is testConfig plus a configured agentic runner. Separate from
// testConfig so every existing test keeps asserting the shape of a pod on an
// install that has never heard of runners — which is the shape almost every
// deployment will keep having.
func agenticConfig() Config {
	cfg := testConfig("octo.example.com")
	cfg.AgenticRunnerImage = "octo-agenticrunner:dev"
	cfg.AgenticRunnerWorkspaceSize = "100Mi"
	cfg.AgenticRunnerResources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
	}
	return cfg
}

func TestParseRunner(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    Runner
		wantErr bool
	}{
		{in: "", want: RunnerStandard},
		{in: "standard", want: RunnerStandard},
		{in: "agentic", want: RunnerAgentic},
		// A typo must not resolve to the default. Deploying the distroless image for
		// "agentik" produces a pod whose every command fails with "not found", which
		// nobody would trace back to a misspelled setting.
		{in: "agentik", wantErr: true},
		{in: "Agentic", wantErr: true},
	} {
		got, err := ParseRunner(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseRunner(%q) = %q, want an error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRunner(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseRunner(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestRunnerEnabledFollowsTheConfiguredImage pins the predicate the deployment
// service refuses on. The standard runner is always available; the agentic one
// exists only where the chart named an image for it.
func TestRunnerEnabledFollowsTheConfiguredImage(t *testing.T) {
	off := testClientFor(testConfig("octo.example.com"))
	if !off.RunnerEnabled(RunnerStandard) {
		t.Error("standard runner should always be enabled")
	}
	if off.RunnerEnabled(RunnerAgentic) {
		t.Error("agentic runner enabled with no image configured")
	}

	on := testClientFor(agenticConfig())
	if !on.RunnerEnabled(RunnerAgentic) {
		t.Error("agentic runner disabled with an image configured")
	}
}

// TestAgenticRunnerPodShape is the whole of what a runner changes: the image, a
// writable workspace with a size limit, and resources.
func TestAgenticRunnerPodShape(t *testing.T) {
	c := testClientFor(agenticConfig())
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "i1", Definition: "service:\n  name: x\n", Runner: RunnerAgentic}
	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	dep, err := c.clientset.AppsV1().Deployments(testNamespace).
		Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	pod := dep.Spec.Template.Spec
	container := pod.Containers[0]

	if container.Image != "octo-agenticrunner:dev" {
		t.Errorf("image = %q, want the agentic runner", container.Image)
	}

	var workspace *corev1.Volume
	for i := range pod.Volumes {
		if pod.Volumes[i].Name == workspaceVolume {
			workspace = &pod.Volumes[i]
		}
	}
	if workspace == nil {
		t.Fatalf("no %q volume; volumes = %+v", workspaceVolume, pod.Volumes)
	}
	if workspace.EmptyDir == nil {
		t.Fatalf("%q volume is not an emptyDir: %+v", workspaceVolume, workspace.VolumeSource)
	}
	// The size limit is what makes a shell safe to hand out: without it the
	// workspace is bounded only by the node's disk, so one runaway command evicts
	// every pod on the node rather than only this one.
	if workspace.EmptyDir.SizeLimit == nil {
		t.Fatal("workspace emptyDir has no SizeLimit")
	}
	if got := workspace.EmptyDir.SizeLimit.String(); got != "100Mi" {
		t.Errorf("workspace SizeLimit = %s, want 100Mi", got)
	}

	var mount *corev1.VolumeMount
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].Name == workspaceVolume {
			mount = &container.VolumeMounts[i]
		}
	}
	if mount == nil {
		t.Fatalf("workspace not mounted; mounts = %+v", container.VolumeMounts)
	}
	if mount.MountPath != workspaceMountPath {
		t.Errorf("workspace mounted at %q, want %q", mount.MountPath, workspaceMountPath)
	}
	// Writable, unlike the ConfigMap beside it. A read-only workspace would be a
	// lie about who owns the directory, and would break every file-write the
	// runner exists to make possible.
	if mount.ReadOnly {
		t.Error("workspace mounted read-only")
	}

	if container.Resources.Limits.Memory().String() != "1Gi" {
		t.Errorf("memory limit = %s, want 1Gi", container.Resources.Limits.Memory())
	}
}

// TestStandardRunnerPodIsUnchanged is the regression that matters most: every
// integration that already exists must keep exactly the pod it has, on an install
// that configures the agentic runner as much as on one that does not.
func TestStandardRunnerPodIsUnchanged(t *testing.T) {
	c := testClientFor(agenticConfig())
	ctx := context.Background()
	// No Runner set — the zero value, which is what every stored deployment row
	// written before this feature decodes to.
	spec := Spec{ID: "d2", IntegrationID: "i1", Definition: "service:\n  name: x\n"}
	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	dep, err := c.clientset.AppsV1().Deployments(testNamespace).
		Get(ctx, resourceName("d2"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	pod := dep.Spec.Template.Spec
	container := pod.Containers[0]

	if container.Image != "octo-runtime:dev" {
		t.Errorf("image = %q, want the standard runtime", container.Image)
	}
	if len(pod.Volumes) != 1 || pod.Volumes[0].Name != "integration" {
		t.Errorf("volumes = %+v, want only the integration ConfigMap", pod.Volumes)
	}
	if len(container.VolumeMounts) != 1 || container.VolumeMounts[0].Name != "integration" {
		t.Errorf("mounts = %+v, want only the integration ConfigMap", container.VolumeMounts)
	}
	if len(container.Resources.Requests) != 0 || len(container.Resources.Limits) != 0 {
		t.Errorf("resources = %+v, want none", container.Resources)
	}
}

// TestAgenticRunnerFallsBackWhenUnconfigured pins the belt to the deployment
// service's brace. Reaching here with an unconfigured runner should be impossible
// — RunnerEnabled refuses first — but if it ever happens, the standard image is
// the answer that produces a running pod rather than an ImagePullBackOff on an
// image name that is empty.
func TestAgenticRunnerFallsBackWhenUnconfigured(t *testing.T) {
	c := testClientFor(testConfig("octo.example.com"))
	if got := c.RunnerImage(RunnerAgentic); got != "octo-runtime:dev" {
		t.Errorf("unconfigured agentic image = %q, want the standard runtime", got)
	}
}

// TestWorkspaceSizeFallsBackToTheDefault covers both halves of parseWorkspaceSize:
// an unset value and one nobody can parse both land on the default rather than on
// a zero Quantity, which Kubernetes reads as "no limit at all".
func TestWorkspaceSizeFallsBackToTheDefault(t *testing.T) {
	for _, in := range []string{"", "not-a-quantity"} {
		cfg := agenticConfig()
		cfg.AgenticRunnerWorkspaceSize = in
		c := testClientFor(cfg)
		if got := c.workspaceSize.String(); got != defaultWorkspaceSize {
			t.Errorf("workspaceSize for %q = %s, want %s", in, got, defaultWorkspaceSize)
		}
	}
}

// TestWorkspaceSizeRejectsANonPositiveCap is the one that matters most here.
// "0" and "-1" are perfectly valid Quantities, and kubelet reads a zero SizeLimit
// as NO limit — so accepting one would silently produce the unbounded workspace
// the cap exists to prevent, on the single workload whose purpose is running other
// programs.
func TestWorkspaceSizeRejectsANonPositiveCap(t *testing.T) {
	for _, in := range []string{"0", "-1", "-100Mi"} {
		cfg := agenticConfig()
		cfg.AgenticRunnerWorkspaceSize = in
		if err := cfg.Validate(); err == nil {
			t.Errorf("Validate accepted a workspace size of %q", in)
		}
		if got := testClientFor(cfg).workspaceSize.String(); got != defaultWorkspaceSize {
			t.Errorf("workspaceSize for %q = %s, want the default %s", in, got, defaultWorkspaceSize)
		}
	}
}

// TestValidateRejectsAnUnparseableWorkspaceSize — the size still falls back at
// runtime, but the operator is told they mistyped it rather than left wondering
// why their 5Gi workspace is 100Mi.
func TestValidateRejectsAnUnparseableWorkspaceSize(t *testing.T) {
	cfg := agenticConfig()
	cfg.AgenticRunnerWorkspaceSize = "100 megabytes"
	if err := cfg.Validate(); err == nil {
		t.Error("Validate accepted a workspace size that is not a quantity")
	}
}
