package kube

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const testNamespace = "octo-dev"

// testClient builds a Client backed by a fake clientset, pre-seeded with objects.
// baseDomain is set so external endpoints are enabled; pass "" via newClient for
// the disabled case.
func testClient(objects ...runtime.Object) *Client {
	return newClient("octo.example.com", objects...)
}

func newClient(baseDomain string, objects ...runtime.Object) *Client {
	return &Client{
		clientset:     fake.NewSimpleClientset(objects...),
		namespace:     testNamespace,
		runtimeImage:  "octo-runtime:dev",
		baseDomain:    baseDomain,
		clusterIssuer: "letsencrypt-prod",
	}
}

func TestSpecPortDefaultsToRuntimePort(t *testing.T) {
	if got := (Spec{}).port(); got != runtimePort {
		t.Errorf("zero Spec port = %d, want %d", got, runtimePort)
	}
	if got := (Spec{Port: 9090}).port(); got != 9090 {
		t.Errorf("declared port = %d, want 9090", got)
	}
}

// TestApplyNoServiceWhenNoPort verifies a deployment with no HTTP source (Port 0)
// gets only a ConfigMap + Deployment — no per-deployment Service, no internal
// Service, no Ingress, and no declared container port.
func TestApplyNoServiceWhenNoPort(t *testing.T) {
	c := testClient()
	ctx := context.Background()
	// Slug set but Port 0: the service layer never produces this pairing, but it
	// asserts the port — not the slug — gates Service creation.
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 2, Slug: "orders"}

	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	name := resourceName("d1")
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	ctr := dep.Spec.Template.Spec.Containers[0]
	if len(ctr.Env) != 0 {
		t.Errorf("expected no env vars by default, got %v", ctr.Env)
	}
	if len(ctr.Ports) != 0 {
		t.Errorf("expected no container port for a non-networked workload, got %v", ctr.Ports)
	}

	if _, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Error("per-deployment service should not exist for a non-networked workload")
	}
	if _, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, internalServiceName("orders"), metav1.GetOptions{}); err == nil {
		t.Error("internal service should not exist for a non-networked workload")
	}
	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Error("ingress should not exist for a non-networked workload")
	}
}

func TestApplyDeclaredPortAndEnv(t *testing.T) {
	c := testClient()
	ctx := context.Background()
	spec := Spec{
		ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1, Slug: "orders",
		Port: 9090,
		Env:  map[string]string{"HTTP_PORT": "9090", "HTTP_HOST": "0.0.0.0"},
	}

	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	name := resourceName("d1")
	svc, _ := c.clientset.CoreV1().Services(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if svc.Spec.Ports[0].Port != 9090 || svc.Spec.Ports[0].TargetPort.IntValue() != 9090 {
		t.Errorf("service port/target = %d/%d, want 9090/9090", svc.Spec.Ports[0].Port, svc.Spec.Ports[0].TargetPort.IntValue())
	}

	internal, _ := c.clientset.CoreV1().Services(testNamespace).Get(ctx, internalServiceName("orders"), metav1.GetOptions{})
	if internal.Spec.Ports[0].Port != 9090 {
		t.Errorf("internal service port = %d, want 9090", internal.Spec.Ports[0].Port)
	}
	// The internal Service is per-deployment: it selects this deployment's pods.
	if internal.Spec.Selector[labelDeploymentID] != "d1" {
		t.Errorf("internal service selector = %v, want deployment-id d1", internal.Spec.Selector)
	}

	dep, _ := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, name, metav1.GetOptions{})
	ctr := dep.Spec.Template.Spec.Containers[0]
	if ctr.Ports[0].ContainerPort != 9090 {
		t.Errorf("container port = %d, want 9090", ctr.Ports[0].ContainerPort)
	}
	// Env is sorted by name: HTTP_HOST then HTTP_PORT.
	wantEnv := map[string]string{"HTTP_HOST": "0.0.0.0", "HTTP_PORT": "9090"}
	if len(ctr.Env) != 2 {
		t.Fatalf("container env = %v, want 2 vars", ctr.Env)
	}
	for _, e := range ctr.Env {
		if wantEnv[e.Name] != e.Value {
			t.Errorf("env %s = %q, want %q", e.Name, e.Value, wantEnv[e.Name])
		}
	}
	if ctr.Env[0].Name != "HTTP_HOST" || ctr.Env[1].Name != "HTTP_PORT" {
		t.Errorf("env not sorted by name: %v", ctr.Env)
	}
}

// TestContainerEnvMergesLiteralsAndSecrets verifies the container env combines
// literal values and secretKeyRef references into one slice sorted by name, with
// the secret refs pointing at the shared cluster-secrets Secret.
func TestContainerEnvMergesLiteralsAndSecrets(t *testing.T) {
	spec := Spec{
		Env:       map[string]string{"HTTP_PORT": "9090", "DB_HOST": "db"},
		SecretEnv: map[string]string{"API_KEY": "API_KEY", "TOKEN": "SHARED_TOKEN"},
	}
	env := containerEnv(spec)
	if len(env) != 4 {
		t.Fatalf("env len = %d, want 4: %+v", len(env), env)
	}
	// Sorted by name: API_KEY, DB_HOST, HTTP_PORT, TOKEN.
	wantOrder := []string{"API_KEY", "DB_HOST", "HTTP_PORT", "TOKEN"}
	for i, name := range wantOrder {
		if env[i].Name != name {
			t.Errorf("env[%d] = %q, want %q (not sorted): %+v", i, env[i].Name, name, env)
		}
	}
	// DB_HOST / HTTP_PORT are literals.
	if env[1].Value != "db" || env[1].ValueFrom != nil {
		t.Errorf("DB_HOST should be a literal, got %+v", env[1])
	}
	// API_KEY / TOKEN are secretKeyRefs into octo-secrets, key = mapped value.
	ref := env[0].ValueFrom
	if ref == nil || ref.SecretKeyRef == nil {
		t.Fatalf("API_KEY should be a secretKeyRef, got %+v", env[0])
	}
	if ref.SecretKeyRef.Name != secretsName || ref.SecretKeyRef.Key != "API_KEY" {
		t.Errorf("API_KEY ref = %s/%s, want %s/API_KEY", ref.SecretKeyRef.Name, ref.SecretKeyRef.Key, secretsName)
	}
	if ref.SecretKeyRef.Optional == nil || *ref.SecretKeyRef.Optional {
		t.Error("secret ref should be Optional=false (fail loud on missing key)")
	}
	if env[3].ValueFrom.SecretKeyRef.Key != "SHARED_TOKEN" {
		t.Errorf("TOKEN should map to key SHARED_TOKEN, got %q", env[3].ValueFrom.SecretKeyRef.Key)
	}
	if containerEnv(Spec{}) != nil {
		t.Error("empty spec should yield nil env")
	}
}

// TestRuntimeServicesEnvInjected verifies that, when the client is configured with
// runtime services, every deployed pod gets the backend selector, the deployment id
// and orchestrator URL as literals, POD_NAME/POD_NAMESPACE from the downward API,
// and the runtime ServiceAccount — all ahead of the user's own env.
func TestRuntimeServicesEnvInjected(t *testing.T) {
	c := newClient("")
	c.runtimeServices = RuntimeServices{
		Module:          "k8s",
		OrchestratorURL: "http://octo-orchestrator.octo-dev:8090",
		ServiceAccount:  "octo-runtime",
	}
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1, Env: map[string]string{"LOG_LEVEL": "debug"}}
	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if got := dep.Spec.Template.Spec.ServiceAccountName; got != "octo-runtime" {
		t.Errorf("serviceAccountName = %q, want octo-runtime", got)
	}

	env := dep.Spec.Template.Spec.Containers[0].Env
	byName := map[string]corev1.EnvVar{}
	for _, e := range env {
		byName[e.Name] = e
	}
	if byName[envServicesModule].Value != "k8s" {
		t.Errorf("%s = %q, want k8s", envServicesModule, byName[envServicesModule].Value)
	}
	if byName[envDeploymentID].Value != "d1" {
		t.Errorf("%s = %q, want d1", envDeploymentID, byName[envDeploymentID].Value)
	}
	if byName[envOrchestrator].Value != "http://octo-orchestrator.octo-dev:8090" {
		t.Errorf("%s = %q, want the orchestrator URL", envOrchestrator, byName[envOrchestrator].Value)
	}
	for _, name := range []string{envPodName, envPodNamespace} {
		ref := byName[name].ValueFrom
		if ref == nil || ref.FieldRef == nil {
			t.Errorf("%s should be a downward-API fieldRef, got %+v", name, byName[name])
		}
	}
	if byName[envPodName].ValueFrom.FieldRef.FieldPath != "metadata.name" {
		t.Errorf("%s fieldPath = %q, want metadata.name", envPodName, byName[envPodName].ValueFrom.FieldRef.FieldPath)
	}
	// Injected vars precede the user's own env so the spec is deterministic.
	if env[len(env)-1].Name != "LOG_LEVEL" {
		t.Errorf("user env should come last, got order %v", env)
	}
}

// TestRuntimeServicesEnvDisabledByDefault verifies the zero-value config injects
// nothing: no runtime-services env and the default ServiceAccount.
func TestRuntimeServicesEnvDisabledByDefault(t *testing.T) {
	c := newClient("")
	ctx := context.Background()
	if err := c.Apply(ctx, Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	dep, _ := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if got := dep.Spec.Template.Spec.ServiceAccountName; got != "" {
		t.Errorf("serviceAccountName = %q, want empty (default SA)", got)
	}
	if env := dep.Spec.Template.Spec.Containers[0].Env; len(env) != 0 {
		t.Errorf("expected no env without runtime services, got %v", env)
	}
}

// TestSecretLifecycle exercises set/list/exists/delete on the shared Secret.
func TestSecretLifecycle(t *testing.T) {
	c := testClient()
	ctx := context.Background()

	if names, err := c.ListSecretNames(ctx); err != nil || len(names) != 0 {
		t.Fatalf("empty list = %v, %v; want none", names, err)
	}
	if ok, _ := c.SecretKeyExists(ctx, "API_KEY"); ok {
		t.Error("key should not exist before set")
	}

	// First set creates the Secret; second set adds a key; third overwrites.
	if err := c.SetSecret(ctx, "API_KEY", "v1"); err != nil {
		t.Fatalf("set API_KEY: %v", err)
	}
	if err := c.SetSecret(ctx, "TOKEN", "t1"); err != nil {
		t.Fatalf("set TOKEN: %v", err)
	}
	if err := c.SetSecret(ctx, "API_KEY", "v2"); err != nil {
		t.Fatalf("overwrite API_KEY: %v", err)
	}

	sec, err := c.clientset.CoreV1().Secrets(testNamespace).Get(ctx, secretsName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if string(sec.Data["API_KEY"]) != "v2" {
		t.Errorf("API_KEY = %q, want overwritten v2", sec.Data["API_KEY"])
	}

	names, err := c.ListSecretNames(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 2 || names[0] != "API_KEY" || names[1] != "TOKEN" {
		t.Errorf("names = %v, want sorted [API_KEY TOKEN]", names)
	}
	if ok, _ := c.SecretKeyExists(ctx, "API_KEY"); !ok {
		t.Error("API_KEY should exist after set")
	}

	if err := c.DeleteSecretKey(ctx, "API_KEY"); err != nil {
		t.Fatalf("delete API_KEY: %v", err)
	}
	if ok, _ := c.SecretKeyExists(ctx, "API_KEY"); ok {
		t.Error("API_KEY should be gone after delete")
	}
	// Deleting a missing key and a key from a present Secret are both no-ops.
	if err := c.DeleteSecretKey(ctx, "API_KEY"); err != nil {
		t.Errorf("delete missing key should be a no-op, got %v", err)
	}
}

func TestApplyCreatesIngressWhenExposed(t *testing.T) {
	c := testClient()
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1, Slug: "orders", Port: 9090, Expose: true, Subdomain: "shop"}

	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	ing, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ingress: %v", err)
	}
	rule := ing.Spec.Rules[0]
	if rule.Host != "shop.octo.example.com" {
		t.Errorf("ingress host = %q, want shop.octo.example.com", rule.Host)
	}
	if got := rule.HTTP.Paths[0].Backend.Service.Port.Number; got != 9090 {
		t.Errorf("ingress backend port = %d, want 9090", got)
	}
	if ing.Annotations["cert-manager.io/cluster-issuer"] != "letsencrypt-prod" {
		t.Errorf("missing cluster-issuer annotation: %v", ing.Annotations)
	}
	if ing.Spec.TLS[0].Hosts[0] != "shop.octo.example.com" {
		t.Errorf("TLS host = %v, want shop.octo.example.com", ing.Spec.TLS)
	}
}

func TestApplyIngressUsesWildcardSecret(t *testing.T) {
	c := newClient("octo.example.com")
	c.wildcardTLSSecret = "octo-wildcard-tls"
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1, Slug: "orders", Port: 9090, Expose: true, Subdomain: "shop"}

	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	ing, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ingress: %v", err)
	}
	// The wildcard cert is managed by a standalone Certificate, so the per-host
	// cert-manager annotation must be absent and TLS must reference the shared Secret.
	if _, ok := ing.Annotations["cert-manager.io/cluster-issuer"]; ok {
		t.Errorf("wildcard mode must not set the cluster-issuer annotation: %v", ing.Annotations)
	}
	if got := ing.Spec.TLS[0].SecretName; got != "octo-wildcard-tls" {
		t.Errorf("TLS secret = %q, want octo-wildcard-tls", got)
	}
	if ing.Spec.TLS[0].Hosts[0] != "shop.octo.example.com" {
		t.Errorf("TLS host = %v, want shop.octo.example.com", ing.Spec.TLS)
	}
}

func TestApplyNoIngressWithoutBaseDomain(t *testing.T) {
	c := newClient("") // external disabled
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1, Slug: "orders", Port: 9090, Expose: true, Subdomain: "shop"}

	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{}); err == nil {
		t.Error("ingress should not be created when no base domain is configured")
	}
}

func TestInternalURL(t *testing.T) {
	c := newClient("")
	if got := c.InternalURL("", 0); got != "" {
		t.Errorf("empty slug should yield empty URL, got %q", got)
	}
	if got := c.InternalURL("orders", 0); got != "http://octo-int-orders.octo-dev:8080" {
		t.Errorf("default port URL = %q", got)
	}
	if got := c.InternalURL("orders", 9090); got != "http://octo-int-orders.octo-dev:9090" {
		t.Errorf("declared port URL = %q", got)
	}
}

func TestExternalHostAndURL(t *testing.T) {
	c := testClient()
	if !c.ExternalEnabled() {
		t.Fatal("external should be enabled with a base domain")
	}
	if got := c.ExternalHost("shop"); got != "shop.octo.example.com" {
		t.Errorf("host = %q", got)
	}
	if got := c.ExternalURL("shop"); got != "https://shop.octo.example.com" {
		t.Errorf("url = %q", got)
	}

	off := newClient("")
	if off.ExternalEnabled() || off.ExternalHost("shop") != "" || off.ExternalURL("shop") != "" {
		t.Error("external host/url should be empty when disabled")
	}
}

func TestStatusRunningReportsReplicas(t *testing.T) {
	desired := int32(2)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName("d1"), Namespace: testNamespace},
		Spec:       appsv1.DeploymentSpec{Replicas: &desired},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "d1-pod", Namespace: testNamespace, Labels: map[string]string{labelDeploymentID: "d1"}},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{
				RestartCount: 2,
				State:        corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			}},
		},
	}
	c := testClient(dep, pod)
	got, err := c.Status(context.Background(), "d1")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Phase != StatusRunning {
		t.Errorf("phase = %q, want running", got.Phase)
	}
	if got.DesiredReplicas != 2 || got.ReadyReplicas != 1 {
		t.Errorf("replicas ready/desired = %d/%d, want 1/2", got.ReadyReplicas, got.DesiredReplicas)
	}
	if len(got.Pods) != 1 || !got.Pods[0].Ready || got.Pods[0].Restarts != 2 || got.Pods[0].Phase != "Running" {
		t.Errorf("pod detail = %+v, want one ready Running pod with 2 restarts", got.Pods)
	}
}

func TestStatusPending(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName("d1"), Namespace: testNamespace},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
	}
	c := testClient(dep)
	got, _ := c.Status(context.Background(), "d1")
	if got.Phase != StatusPending {
		t.Errorf("phase = %q, want pending", got.Phase)
	}
}

func TestStatusFailedWhenDeploymentMissing(t *testing.T) {
	c := testClient()
	got, err := c.Status(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Phase != StatusFailed {
		t.Errorf("phase = %q, want failed", got.Phase)
	}
}

func TestStatusFailedOnCrashLoopWithReason(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: resourceName("d1"), Namespace: testNamespace},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
	}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "d1-pod",
			Namespace: testNamespace,
			Labels:    map[string]string{labelDeploymentID: "d1"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "back-off pulling image"},
				},
			}},
		},
	}
	c := testClient(dep, pod)
	got, _ := c.Status(context.Background(), "d1")
	if got.Phase != StatusFailed {
		t.Errorf("phase = %q, want failed", got.Phase)
	}
	if got.Reason != "ImagePullBackOff: back-off pulling image" {
		t.Errorf("reason = %q, want the waiting reason+message", got.Reason)
	}
}

func TestDeleteIgnoresMissing(t *testing.T) {
	// Delete of a never-created deployment is a no-op (all NotFound ignored).
	c := testClient()
	if err := c.Delete(context.Background(), "ghost"); err != nil {
		t.Errorf("Delete of missing resources should succeed, got %v", err)
	}
}

func TestScaleUpdatesReplicas(t *testing.T) {
	c := testClient()
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1, Slug: "orders"}
	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if err := c.Scale(ctx, "d1", 5); err != nil {
		t.Fatalf("Scale: %v", err)
	}
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 5 {
		t.Errorf("replicas = %v, want 5", dep.Spec.Replicas)
	}
}

func TestScaleNormalizesBelowOne(t *testing.T) {
	c := testClient()
	ctx := context.Background()
	if err := c.Apply(ctx, Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 3}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := c.Scale(ctx, "d1", 0); err != nil {
		t.Fatalf("Scale: %v", err)
	}
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, resourceName("d1"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
		t.Errorf("replicas = %v, want normalized to 1", dep.Spec.Replicas)
	}
}

func TestScaleMissingDeployment(t *testing.T) {
	c := testClient()
	if err := c.Scale(context.Background(), "ghost", 2); err == nil {
		t.Error("Scale of a missing deployment should error")
	}
}

func TestEnsureInternalServiceIdempotent(t *testing.T) {
	c := testClient()
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Slug: "orders", Port: 8080}
	if err := c.ensureInternalService(ctx, spec); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	// A second deployment of the same integration must not fail on AlreadyExists.
	spec2 := Spec{ID: "d2", IntegrationID: "int-1", Slug: "orders", Port: 8080}
	if err := c.ensureInternalService(ctx, spec2); err != nil {
		t.Errorf("second ensure should be idempotent, got %v", err)
	}
}
