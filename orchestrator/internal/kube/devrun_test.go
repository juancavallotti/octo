package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

// devRunConfig is the configuration a dev-run test starts from: testConfig plus the
// three settings dev runs need, without which they are disabled.
func devRunConfig(baseDomain string) Config {
	cfg := testConfig(baseDomain)
	cfg.DevRuntimeImage = "octo-devruntime:dev"
	cfg.SidecarImage = "octo-devsidecar:dev"
	cfg.OrchestratorURL = "http://octo-orchestrator:8090"
	return cfg
}

// devRunClient builds a Client with dev runs enabled and external endpoints on.
func devRunClient(objects ...runtime.Object) *Client {
	return testClientFor(devRunConfig("apps.example.com"), objects...)
}

// testDevRunSpec is a networked dev run for user u1 editing integration int-1.
func testDevRunSpec() DevRunSpec {
	return DevRunSpec{
		ID:            "11111111-1111-4111-8111-111111111111",
		UserID:        "u1",
		IntegrationID: "int-1",
		DevRunToken:   "pull-token",
		TokenHash:     "hash-of-pull-token",
		SidecarToken:  "command-token",
		Networked:     true,
		Subdomain:     "abc12def",
	}
}

// getDevRunDeployment fetches a dev run's Deployment, failing the test if absent.
func getDevRunDeployment(t *testing.T, c *Client, id string) *appsv1.Deployment {
	t.Helper()
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(
		context.Background(), devRunName(id), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get dev run deployment: %v", err)
	}
	return dep
}

// tombstoneOf wraps an object the way an informer delivers a delete it observed
// only after the fact, so the guard is tested on both shapes it can receive.
func tombstoneOf(obj any) cache.DeletedFinalStateUnknown {
	return cache.DeletedFinalStateUnknown{Key: "octo-dev/whatever", Obj: obj}
}

// envOf indexes a container's env by name for readable assertions.
func envOf(ctr corev1.Container) map[string]string {
	out := make(map[string]string, len(ctr.Env))
	for _, e := range ctr.Env {
		out[e.Name] = e.Value
	}
	return out
}

func TestApplyDevRunCreatesTwoContainerPod(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()

	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	dep := getDevRunDeployment(t, c, spec.ID)
	pod := dep.Spec.Template.Spec

	// The sidecar is a NATIVE sidecar: an initContainers entry with restartPolicy
	// Always. That is what makes the runtime container start only after the first
	// pull, which `octo run --watch` needs — it exits if the watched directory is
	// missing.
	if len(pod.InitContainers) != 1 {
		t.Fatalf("init containers = %d, want 1 (the native sidecar)", len(pod.InitContainers))
	}
	sidecar := pod.InitContainers[0]
	if sidecar.RestartPolicy == nil || *sidecar.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Errorf("sidecar restartPolicy = %v, want Always — without it this is a plain "+
			"init container that runs once and exits", sidecar.RestartPolicy)
	}
	if sidecar.StartupProbe == nil {
		t.Fatal("sidecar has no startupProbe; nothing then orders the runtime's start")
	}
	if got := sidecar.StartupProbe.HTTPGet.Path; got != sidecarReadyPath {
		t.Errorf("startupProbe path = %q, want %q", got, sidecarReadyPath)
	}

	if len(pod.Containers) != 1 {
		t.Fatalf("containers = %d, want 1 (the runtime)", len(pod.Containers))
	}
	rt := pod.Containers[0]
	// --watch, and pointed at the shared workspace rather than the image's default
	// ConfigMap path.
	wantArgs := []string{"run", "--config", devWorkspaceDir, "--watch"}
	if len(rt.Args) != len(wantArgs) {
		t.Fatalf("runtime args = %v, want %v", rt.Args, wantArgs)
	}
	for i, want := range wantArgs {
		if rt.Args[i] != want {
			t.Errorf("runtime args = %v, want %v", rt.Args, wantArgs)
			break
		}
	}
}

// The workspace is one emptyDir both containers mount. The subPath on the runtime
// side is the belt to the sidecar's brace: kubelet creates the subdirectory, so the
// directory exists even if the sidecar has not written it.
func TestApplyDevRunSharesWorkspaceVolume(t *testing.T) {
	c := devRunClient()
	if err := c.ApplyDevRun(context.Background(), testDevRunSpec()); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}
	pod := getDevRunDeployment(t, c, testDevRunSpec().ID).Spec.Template.Spec

	if len(pod.Volumes) != 1 || pod.Volumes[0].EmptyDir == nil {
		t.Fatalf("volumes = %+v, want one emptyDir", pod.Volumes)
	}
	if pod.Volumes[0].Name != devWorkspaceVolume {
		t.Errorf("volume name = %q, want %q", pod.Volumes[0].Name, devWorkspaceVolume)
	}

	side := pod.InitContainers[0].VolumeMounts
	if len(side) != 1 || side[0].MountPath != devWorkspaceRoot || side[0].SubPath != "" {
		t.Errorf("sidecar mount = %+v, want the whole volume at %s", side, devWorkspaceRoot)
	}
	rt := pod.Containers[0].VolumeMounts
	if len(rt) != 1 || rt[0].MountPath != devWorkspaceDir || rt[0].SubPath != devWorkspaceSubPath {
		t.Errorf("runtime mount = %+v, want %s with subPath %s", rt, devWorkspaceDir, devWorkspaceSubPath)
	}
	if rt[0].ReadOnly {
		t.Error("runtime mount is read-only; it shares the volume the sidecar writes")
	}
}

// The listen port is a platform constant injected as HTTP_PORT, which the runtime
// resolves ahead of the definition's declared default. That is what removes any
// need to record or reconcile a port.
func TestApplyDevRunPinsListenPort(t *testing.T) {
	c := devRunClient()
	if err := c.ApplyDevRun(context.Background(), testDevRunSpec()); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}
	pod := getDevRunDeployment(t, c, testDevRunSpec().ID).Spec.Template.Spec
	env := envOf(pod.Containers[0])

	if env[envHTTPPort] != "8080" {
		t.Errorf("HTTP_PORT = %q, want 8080", env[envHTTPPort])
	}
	if env[envHTTPHost] != bindAllHost {
		t.Errorf("HTTP_HOST = %q, want %q — otherwise the pod is unreachable through its Service",
			env[envHTTPHost], bindAllHost)
	}
}

// A non-networked integration (a cron flow) gets no HTTP port and no endpoint — but
// it still gets a Service, because that is how the orchestrator reaches the sidecar
// to reload it.
func TestApplyDevRunNotNetworked(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	spec.Networked = false
	ctx := context.Background()

	if err := c.ApplyDevRun(ctx, spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	pod := getDevRunDeployment(t, c, spec.ID).Spec.Template.Spec
	env := envOf(pod.Containers[0])
	if _, ok := env[envHTTPPort]; ok {
		t.Errorf("HTTP_PORT injected for a non-networked run: %q", env[envHTTPPort])
	}
	for _, p := range pod.Containers[0].Ports {
		if p.Name == "http" {
			t.Error("runtime declares an http port for a non-networked run")
		}
	}

	svc, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, devRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v — a non-networked dev run still needs one for the sidecar", err)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Name != sidecarPortName {
		t.Errorf("service ports = %+v, want only the sidecar port", svc.Spec.Ports)
	}

	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(
		ctx, devRunName(spec.ID), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("get ingress: %v, want NotFound for a non-networked run", err)
	}
}

func TestApplyDevRunPublishesEndpoint(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	ctx := context.Background()

	if err := c.ApplyDevRun(ctx, spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	ing, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, devRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ingress: %v", err)
	}
	if got, want := ing.Spec.Rules[0].Host, "abc12def.apps.example.com"; got != want {
		t.Errorf("host = %q, want %q", got, want)
	}

	// The Service carries both ports: the sidecar's for commands, the runtime's for
	// the endpoint to target.
	svc, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, devRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if len(svc.Spec.Ports) != 2 {
		t.Errorf("service ports = %+v, want the sidecar port and the http port", svc.Spec.Ports)
	}
}

// Two editor tabs on the same integration are meant to share one pod. The workload
// name is derived, so the second Apply hits an existing object — and that is an
// "attached" answer, not a failure.
func TestApplyDevRunIsIdempotent(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	ctx := context.Background()

	if err := c.ApplyDevRun(ctx, spec); err != nil {
		t.Fatalf("first ApplyDevRun: %v", err)
	}
	err := c.ApplyDevRun(ctx, spec)
	if !errors.Is(err, ErrDevRunExists) {
		t.Fatalf("second ApplyDevRun: %v, want ErrDevRunExists", err)
	}

	deps, err := c.clientset.AppsV1().Deployments(testNamespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deps.Items) != 1 {
		t.Errorf("deployments = %d, want 1 — the second Run must attach, not start a second pod", len(deps.Items))
	}
}

// The token's hash lives on the workload, not in a table. That is what makes
// authorisation precise: when the workload goes, the credential stops working.
func TestApplyDevRunRecordsTokenHashNotToken(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	dep := getDevRunDeployment(t, c, spec.ID)
	if got := dep.Annotations[annTokenHash]; got != spec.TokenHash {
		t.Errorf("token-hash annotation = %q, want %q", got, spec.TokenHash)
	}
	for k, v := range dep.Annotations {
		if v == spec.DevRunToken {
			t.Errorf("annotation %q carries the plaintext token", k)
		}
	}
	// The plaintext belongs in the sidecar's env and nowhere else.
	if got := envOf(dep.Spec.Template.Spec.InitContainers[0])[envDevRunToken]; got != spec.DevRunToken {
		t.Errorf("sidecar DEV_RUN_TOKEN = %q, want the plaintext token", got)
	}
}

// The two tokens are distinct so a leak in one direction does not compromise the
// other: one authorises the pod's outbound pull, the other the orchestrator's
// inbound commands.
func TestSidecarEnvKeepsTokensDistinct(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}
	env := envOf(getDevRunDeployment(t, c, spec.ID).Spec.Template.Spec.InitContainers[0])

	if env[envDevRunToken] == env[envSidecarToken] {
		t.Error("DEV_RUN_TOKEN and SIDECAR_TOKEN are the same value")
	}
	if env[envWorkspaceDir] != devWorkspaceDir {
		t.Errorf("WORKSPACE_DIR = %q, want %q", env[envWorkspaceDir], devWorkspaceDir)
	}
	// The two containers share a network namespace, so the admin port is loopback.
	if want := "127.0.0.1:39999"; env[envRuntimeAdmin] != want {
		t.Errorf("RUNTIME_ADMIN_ADDR = %q, want %q", env[envRuntimeAdmin], want)
	}
	if env[envDevRunID] != spec.ID {
		t.Errorf("DEV_RUN_ID = %q, want %q", env[envDevRunID], spec.ID)
	}
}

// A dev run uses the STANDALONE runtime image and gets none of the cluster wiring a
// deployment does. This is the test that pins that apart, because the two images are
// mutually exclusive builds and running the wrong one is a pod that cannot work: the
// k8s build has no standalone provider, so with no RUNTIME_SERVICES_MODULE it has
// nothing to select.
//
// The configuration below is the trap — full runtime-services wiring present — and
// none of it may reach a dev-run pod.
func TestDevRunUsesStandaloneRuntimeWithNoClusterWiring(t *testing.T) {
	cfg := devRunConfig("apps.example.com")
	cfg.RuntimeImage = "octo-runtime-paas:dev"
	cfg.RuntimeServices = RuntimeServices{
		Module:          "k8s",
		OrchestratorURL: "http://octo-orchestrator:8090",
		ServiceAccount:  "octo-runtime",
		NATSURL:         "nats://octo-nats:4222",
	}
	c := testClientFor(cfg)
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	pod := getDevRunDeployment(t, c, spec.ID).Spec.Template.Spec
	rt := pod.Containers[0]

	if rt.Image != "octo-devruntime:dev" {
		t.Errorf("runtime image = %q, want the standalone build — the k8s build cannot run "+
			"without the orchestrator, the queues and the aggregator", rt.Image)
	}
	env := envOf(rt)
	for _, name := range []string{
		envServicesModule, envDeploymentID, envDeploymentName,
		envDeploymentVer, envSnapshotID, envNATSURL,
	} {
		if v, ok := env[name]; ok {
			t.Errorf("%s = %q on a dev run; the standalone build needs no cluster wiring", name, v)
		}
	}
	// No leader election and no cluster API access, so no credential either.
	if pod.ServiceAccountName != "" {
		t.Errorf("serviceAccountName = %q, want empty — a dev pod needs no RBAC", pod.ServiceAccountName)
	}
	// The only env it does get is where to listen.
	if len(env) != 2 {
		t.Errorf("runtime env = %v, want only HTTP_PORT and HTTP_HOST", env)
	}
}

// A non-networked dev run gets no env at all: nothing to bind, nothing to wire.
func TestDevRunNotNetworkedHasNoEnv(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	spec.Networked = false
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}
	if env := getDevRunDeployment(t, c, spec.ID).Spec.Template.Spec.Containers[0].Env; len(env) != 0 {
		t.Errorf("runtime env = %v, want none", env)
	}
}

// Labels are the record: who owns it, what they are editing, and — via the dev-run
// id — that this is a dev run rather than a deployment.
func TestDevRunLabels(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	dep := getDevRunDeployment(t, c, spec.ID)
	want := map[string]string{
		labelManagedBy:     managedByValue,
		labelDevRunID:      spec.ID,
		labelUserID:        spec.UserID,
		labelIntegrationID: spec.IntegrationID,
	}
	for k, v := range want {
		if got := dep.Labels[k]; got != v {
			t.Errorf("label %s = %q, want %q", k, got, v)
		}
	}
	// No deployment-id label: that is what would make the informers treat this as a
	// deployment.
	if got, ok := dep.Labels[labelDeploymentID]; ok {
		t.Errorf("dev run carries a deployment-id label (%q)", got)
	}
}

func TestTouchDevRunUpdatesAnnotation(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	spec.LastActivity = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()
	if err := c.ApplyDevRun(ctx, spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	at := time.Date(2026, 8, 5, 12, 30, 0, 0, time.UTC)
	if err := c.TouchDevRun(ctx, spec.ID, at); err != nil {
		t.Fatalf("TouchDevRun: %v", err)
	}
	if got := getDevRunDeployment(t, c, spec.ID).Annotations[annLastActivity]; got != "2026-08-05T12:30:00Z" {
		t.Errorf("last-activity = %q, want 2026-08-05T12:30:00Z", got)
	}
}

// A reload that touched nothing means the caller believes in a dev run that does not
// exist, which is worth an error rather than a silent success.
func TestTouchDevRunMissingIsAnError(t *testing.T) {
	c := devRunClient()
	if err := c.TouchDevRun(context.Background(), "nope", time.Now()); err == nil {
		t.Error("TouchDevRun on a missing dev run: want an error")
	}
}

func TestListDevRunsFiltersByUserAndIntegration(t *testing.T) {
	c := devRunClient()
	ctx := context.Background()

	specs := []DevRunSpec{
		{ID: "run-a", UserID: "u1", IntegrationID: "int-1", Networked: true, Subdomain: "aaaaaaaa"},
		{ID: "run-b", UserID: "u1", IntegrationID: "int-2"},
		{ID: "run-c", UserID: "u2", IntegrationID: "int-1"},
	}
	for _, s := range specs {
		if err := c.ApplyDevRun(ctx, s); err != nil {
			t.Fatalf("ApplyDevRun %s: %v", s.ID, err)
		}
	}

	all, err := c.ListDevRuns(ctx, DevRunSelector{})
	if err != nil {
		t.Fatalf("ListDevRuns: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all dev runs = %d, want 3", len(all))
	}

	mine, err := c.ListDevRuns(ctx, DevRunSelector{UserID: "u1"})
	if err != nil {
		t.Fatalf("ListDevRuns by user: %v", err)
	}
	if len(mine) != 2 {
		t.Errorf("u1's dev runs = %d, want 2", len(mine))
	}

	// The Run-vs-Attach question: is something running for THIS integration, for me?
	one, err := c.ListDevRuns(ctx, DevRunSelector{UserID: "u1", IntegrationID: "int-1"})
	if err != nil {
		t.Fatalf("ListDevRuns by user+integration: %v", err)
	}
	if len(one) != 1 || one[0].ID != "run-a" {
		t.Fatalf("u1/int-1 = %+v, want just run-a", one)
	}
	if one[0].Host != "aaaaaaaa.apps.example.com" {
		t.Errorf("host = %q, want the published host off the annotation", one[0].Host)
	}
}

// A deployment carries managed-by and an integration id too, so the listing has to
// exclude it by the absence of a dev-run id — otherwise "what am I running?" reports
// deployed apps as dev runs.
func TestListDevRunsExcludesDeployments(t *testing.T) {
	c := devRunClient()
	ctx := context.Background()

	if err := c.Apply(ctx, Spec{ID: "dep-1", IntegrationID: "int-1", Definition: "x: 1"}); err != nil {
		t.Fatalf("Apply deployment: %v", err)
	}
	if err := c.ApplyDevRun(ctx, DevRunSpec{ID: "run-a", UserID: "u1", IntegrationID: "int-1"}); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	runs, err := c.ListDevRuns(ctx, DevRunSelector{IntegrationID: "int-1"})
	if err != nil {
		t.Fatalf("ListDevRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-a" {
		t.Errorf("dev runs = %+v, want just the dev run", runs)
	}
}

func TestListDevRunsReadsLastActivity(t *testing.T) {
	c := devRunClient()
	ctx := context.Background()
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

	if err := c.ApplyDevRun(ctx, DevRunSpec{ID: "run-a", UserID: "u1", IntegrationID: "int-1", LastActivity: at}); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}
	runs, err := c.ListDevRuns(ctx, DevRunSelector{})
	if err != nil {
		t.Fatalf("ListDevRuns: %v", err)
	}
	if len(runs) != 1 || !runs[0].LastActivity.Equal(at) {
		t.Errorf("last activity = %v, want %v", runs[0].LastActivity, at)
	}
}

// A malformed or absent annotation reads as the zero time rather than failing the
// listing: the reaper decides what a zero means, and one unparseable object must not
// hide every other dev run from a listing.
func TestParseLastActivityTolerates(t *testing.T) {
	if got := parseLastActivity(""); !got.IsZero() {
		t.Errorf("empty = %v, want zero", got)
	}
	if got := parseLastActivity("not a timestamp"); !got.IsZero() {
		t.Errorf("garbage = %v, want zero", got)
	}
	if got := parseLastActivity("2026-08-05T12:30:00Z"); got.IsZero() {
		t.Error("valid timestamp parsed as zero")
	}
}

func TestDeleteDevRunRemovesEverything(t *testing.T) {
	c := devRunClient()
	spec := testDevRunSpec()
	ctx := context.Background()
	if err := c.ApplyDevRun(ctx, spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	if err := c.DeleteDevRun(ctx, spec.ID); err != nil {
		t.Fatalf("DeleteDevRun: %v", err)
	}

	name := devRunName(spec.ID)
	if _, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("deployment still present: %v", err)
	}
	if _, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("service still present: %v", err)
	}
	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("ingress still present: %v", err)
	}
}

// A stop can race a reap, so deleting what is already gone must succeed.
func TestDeleteDevRunIsIdempotent(t *testing.T) {
	c := devRunClient()
	if err := c.DeleteDevRun(context.Background(), "never-existed"); err != nil {
		t.Errorf("DeleteDevRun on a missing dev run: %v, want nil", err)
	}
}

// A definition that gains an HTTP_PORT while running must get its endpoint without
// the pod being recreated.
func TestReconcileDevRunServiceAddsHTTPPort(t *testing.T) {
	c := devRunClient()
	ctx := context.Background()
	spec := testDevRunSpec()
	spec.Networked = false
	if err := c.ApplyDevRun(ctx, spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	spec.Networked = true
	if err := c.ReconcileDevRunService(ctx, spec); err != nil {
		t.Fatalf("ReconcileDevRunService: %v", err)
	}
	if err := c.PublishDevRunEndpoint(ctx, spec); err != nil {
		t.Fatalf("PublishDevRunEndpoint: %v", err)
	}

	svc, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, devRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if len(svc.Spec.Ports) != 2 {
		t.Errorf("service ports = %+v, want the sidecar port plus http", svc.Spec.Ports)
	}
	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, devRunName(spec.ID), metav1.GetOptions{}); err != nil {
		t.Errorf("get ingress: %v", err)
	}

	// Idempotent: a second reconcile must not duplicate the port.
	if err := c.ReconcileDevRunService(ctx, spec); err != nil {
		t.Fatalf("second ReconcileDevRunService: %v", err)
	}
	svc, err = c.clientset.CoreV1().Services(testNamespace).Get(ctx, devRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if len(svc.Spec.Ports) != 2 {
		t.Errorf("service ports after a second reconcile = %+v, want still 2", svc.Spec.Ports)
	}
}

// The reverse flip: an HTTP_PORT removed should stop the public host serving rather
// than leave it pointed at a runtime that no longer listens.
func TestWithdrawDevRunEndpoint(t *testing.T) {
	c := devRunClient()
	ctx := context.Background()
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(ctx, spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}

	if err := c.WithdrawDevRunEndpoint(ctx, spec.ID); err != nil {
		t.Fatalf("WithdrawDevRunEndpoint: %v", err)
	}
	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(
		ctx, devRunName(spec.ID), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("ingress still present: %v", err)
	}
}

// With no base domain there is no host to publish, and the endpoint call is a no-op
// rather than an error, so callers need not check.
func TestPublishDevRunEndpointWithoutBaseDomain(t *testing.T) {
	c := testClientFor(devRunConfig(""))
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}
	ings, err := c.clientset.NetworkingV1().Ingresses(testNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list ingresses: %v", err)
	}
	if len(ings.Items) != 0 {
		t.Errorf("ingresses = %d, want none without a base domain", len(ings.Items))
	}
}

// All three settings are required, and each is tested by its absence: a partial
// configuration produces a pod that fails, which is worse than the feature being off
// with a log line saying why.
func TestDevRunsEnabled(t *testing.T) {
	without := func(clear func(*Config)) Config {
		cfg := devRunConfig("apps.example.com")
		clear(&cfg)
		return cfg
	}

	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"fully configured", devRunConfig("apps.example.com"), true},
		{"no dev runtime image", without(func(c *Config) { c.DevRuntimeImage = "" }), false},
		{"no sidecar image", without(func(c *Config) { c.SidecarImage = "" }), false},
		{"no orchestrator url", without(func(c *Config) { c.OrchestratorURL = "" }), false},
		{"nothing configured", testConfig("apps.example.com"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := testClientFor(tc.cfg).DevRunsEnabled(); got != tc.want {
				t.Errorf("DevRunsEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// A caller that wired only the runtime-services URL still gets working sidecars: the
// two are the same host in every real deployment, so falling back beats silently
// producing pods with nowhere to pull from.
func TestOrchestratorURLFallsBackToRuntimeServices(t *testing.T) {
	cfg := devRunConfig("apps.example.com")
	cfg.OrchestratorURL = ""
	cfg.RuntimeServices.OrchestratorURL = "http://octo-orchestrator:8090"
	c := testClientFor(cfg)

	if !c.DevRunsEnabled() {
		t.Fatal("DevRunsEnabled = false; the fallback did not apply")
	}
	spec := testDevRunSpec()
	if err := c.ApplyDevRun(context.Background(), spec); err != nil {
		t.Fatalf("ApplyDevRun: %v", err)
	}
	env := envOf(getDevRunDeployment(t, c, spec.ID).Spec.Template.Spec.InitContainers[0])
	if env[envOrchestrator] != "http://octo-orchestrator:8090" {
		t.Errorf("sidecar ORCHESTRATOR_URL = %q, want the runtime-services URL", env[envOrchestrator])
	}
}

func TestSidecarPortDefaults(t *testing.T) {
	c := testClientFor(devRunConfig("apps.example.com"))
	if c.sidecarPort != defaultSidecarPort {
		t.Errorf("sidecarPort = %d, want the sidecar's own default %d", c.sidecarPort, defaultSidecarPort)
	}
	cfg := devRunConfig("apps.example.com")
	cfg.SidecarPort = 9100
	if got := testClientFor(cfg).sidecarPort; got != 9100 {
		t.Errorf("sidecarPort = %d, want the configured 9100", got)
	}
}

func TestSidecarURL(t *testing.T) {
	c := devRunClient()
	want := "http://octo-dev-run-a." + testNamespace + ":8099"
	if got := c.SidecarURL("run-a"); got != want {
		t.Errorf("SidecarURL = %q, want %q", got, want)
	}
}

func TestDevRunName(t *testing.T) {
	// Mirrors resourceName's octo-dep- prefix so the two kinds of workload are
	// distinguishable in kubectl output, and stays inside the 63-char DNS label limit.
	id := "11111111-1111-4111-8111-111111111111"
	got := devRunName(id)
	if got != "octo-dev-"+id {
		t.Errorf("devRunName = %q", got)
	}
	if len(got) > 63 {
		t.Errorf("devRunName is %d chars, over the DNS-1123 label limit", len(got))
	}
}

// The informer callback publishes DEPLOYMENT snapshots. A dev run carries the same
// managed-by label and an integration id, so without the dev-run guard every dev-run
// pod transition would announce a deployment change that never happened.
func TestInformerCallbackSkipsDevRuns(t *testing.T) {
	var notified []string
	onChange := func(id string) { notified = append(notified, id) }

	notifyIntegration(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{labelIntegrationID: "int-1", labelDeploymentID: "dep-1"},
	}}, onChange)
	notifyIntegration(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{labelIntegrationID: "int-1", labelDevRunID: "run-a"},
	}}, onChange)

	if len(notified) != 1 || notified[0] != "int-1" {
		t.Errorf("notified = %v, want only the deployment's integration", notified)
	}
}

// The guard also has to hold for the delete tombstone the informer delivers, which
// wraps the object rather than being one.
func TestInformerCallbackSkipsDevRunTombstones(t *testing.T) {
	var notified []string
	notifyIntegration(tombstoneOf(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Labels: map[string]string{labelIntegrationID: "int-1", labelDevRunID: "run-a"},
	}}), func(id string) { notified = append(notified, id) })

	if len(notified) != 0 {
		t.Errorf("notified = %v, want none for a dev-run tombstone", notified)
	}
}
