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

func TestApplyDefaultPortAndNoIngress(t *testing.T) {
	c := testClient()
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 2, Slug: "orders"}

	if err := c.Apply(ctx, spec); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	name := resourceName("d1")
	svc, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if svc.Spec.Ports[0].Port != runtimePort {
		t.Errorf("service port = %d, want %d (default)", svc.Spec.Ports[0].Port, runtimePort)
	}

	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	ctr := dep.Spec.Template.Spec.Containers[0]
	if len(ctr.Env) != 0 {
		t.Errorf("expected no env vars by default, got %v", ctr.Env)
	}
	if ctr.Ports[0].ContainerPort != runtimePort {
		t.Errorf("container port = %d, want %d", ctr.Ports[0].ContainerPort, runtimePort)
	}

	// The stable internal Service is created for a slugged deployment.
	if _, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, internalServiceName("orders"), metav1.GetOptions{}); err != nil {
		t.Errorf("internal service not created: %v", err)
	}

	if _, err := c.clientset.NetworkingV1().Ingresses(testNamespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Error("ingress should not exist for an internal-only deployment")
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

func TestApplyNoIngressWithoutBaseDomain(t *testing.T) {
	c := newClient("") // external disabled
	ctx := context.Background()
	spec := Spec{ID: "d1", IntegrationID: "int-1", Definition: "x: 1", Replicas: 1, Slug: "orders", Expose: true, Subdomain: "shop"}

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
