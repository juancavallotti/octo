package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/juancavallotti/octo/orchestrator/internal/environment/current"
)

func environmentClient(objects ...runtime.Object) *Client {
	cfg := testConfig("")
	cfg.ImagePullSecrets = []string{"registry"}
	cfg.RuntimeServices.ServiceAccount = "octo-runtime"
	cfg.Environments = EnvironmentRBAC{
		OrchestratorServiceAccount: "octo-orchestrator",
		WorkloadClusterRole:        "octo-orchestrator-environment",
		RuntimeClusterRole:         "octo-runtime-environment",
	}
	return testClientFor(cfg, objects...)
}

func TestEnsureNamespaceProvisionsEverythingAndIsRepeatable(t *testing.T) {
	ctx := context.Background()
	pull := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "registry", Namespace: testNamespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{".dockerconfigjson": []byte("{}")},
	}
	c := environmentClient(pull)

	for range 2 {
		if err := c.EnsureNamespace(ctx, "staging", "octo-staging"); err != nil {
			t.Fatalf("EnsureNamespace: %v", err)
		}
	}

	ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, "octo-staging", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace: %v", err)
	}
	if ns.Labels[labelEnvironment] != "staging" {
		t.Errorf("namespace label = %q, want staging", ns.Labels[labelEnvironment])
	}

	orch, err := c.clientset.RbacV1().RoleBindings("octo-staging").Get(ctx, orchestratorBinding, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("orchestrator binding: %v", err)
	}
	if orch.RoleRef.Name != "octo-orchestrator-environment" ||
		orch.Subjects[0].Name != "octo-orchestrator" || orch.Subjects[0].Namespace != testNamespace {
		t.Errorf("orchestrator binding = %+v", orch)
	}
	rt, err := c.clientset.RbacV1().RoleBindings("octo-staging").Get(ctx, runtimeBinding, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("runtime binding: %v", err)
	}
	if rt.RoleRef.Name != "octo-runtime-environment" || rt.Subjects[0].Namespace != "octo-staging" {
		t.Errorf("runtime binding = %+v", rt)
	}
	if _, err := c.clientset.CoreV1().ServiceAccounts("octo-staging").Get(ctx, "octo-runtime", metav1.GetOptions{}); err != nil {
		t.Errorf("runtime service account: %v", err)
	}
	copied, err := c.clientset.CoreV1().Secrets("octo-staging").Get(ctx, "registry", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("pull secret copy: %v", err)
	}
	if copied.Type != corev1.SecretTypeDockerConfigJson || string(copied.Data[".dockerconfigjson"]) != "{}" {
		t.Errorf("pull secret copy = %+v", copied)
	}
}

func TestEnsureNamespaceRefusesSomebodyElsesNamespace(t *testing.T) {
	foreign := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "payments"}}
	c := environmentClient(foreign)
	err := c.EnsureNamespace(context.Background(), "staging", "payments")
	if !errors.Is(err, ErrNamespaceTaken) {
		t.Fatalf("err = %v, want ErrNamespaceTaken", err)
	}
}

func TestDeleteNamespaceOnlyDeletesItsOwn(t *testing.T) {
	ctx := context.Background()
	foreign := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "payments"}}
	c := environmentClient(foreign)
	if err := c.EnsureNamespace(ctx, "staging", "octo-staging"); err != nil {
		t.Fatal(err)
	}

	if err := c.DeleteNamespace(ctx, "staging", "payments"); !errors.Is(err, ErrNamespaceTaken) {
		t.Errorf("deleting a foreign namespace: err = %v, want ErrNamespaceTaken", err)
	}
	if err := c.DeleteNamespace(ctx, "staging", "octo-staging"); err != nil {
		t.Fatalf("DeleteNamespace: %v", err)
	}
	if _, err := c.clientset.CoreV1().Namespaces().Get(ctx, "octo-staging", metav1.GetOptions{}); err == nil {
		t.Error("namespace still exists")
	}
	if err := c.DeleteNamespace(ctx, "staging", "octo-staging"); err != nil {
		t.Errorf("deleting an already-deleted namespace: %v", err)
	}
}

func TestNamespaceInUseSeesManagedWorkloadsOnly(t *testing.T) {
	ctx := context.Background()
	unmanaged := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "octo-staging"}}
	c := environmentClient(unmanaged)

	used, err := c.NamespaceInUse(ctx, "octo-staging")
	if err != nil || used {
		t.Fatalf("with only an unmanaged deployment: used = %v, err = %v", used, err)
	}

	managed := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "octo-dep-1", Namespace: "octo-staging",
		Labels: map[string]string{labelManagedBy: managedByValue},
	}}
	if _, err := c.clientset.AppsV1().Deployments("octo-staging").Create(ctx, managed, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	used, err = c.NamespaceInUse(ctx, "octo-staging")
	if err != nil || !used {
		t.Fatalf("with a managed deployment: used = %v, err = %v", used, err)
	}
}

func TestEnvironmentsEnabledNeedsEveryPart(t *testing.T) {
	if !environmentClient().EnvironmentsEnabled() {
		t.Error("fully configured client reports environments disabled")
	}
	cfg := testConfig("")
	cfg.Environments = EnvironmentRBAC{OrchestratorServiceAccount: "octo-orchestrator"}
	if testClientFor(cfg).EnvironmentsEnabled() {
		t.Error("partly configured client reports environments enabled")
	}
}

func TestNamespaceFollowsTheEnvironmentOnTheContext(t *testing.T) {
	c := environmentClient()
	ctx := context.Background()
	c.RegisterEnvironment("staging", "octo-staging")
	c.ResolveEnvironmentsWith(func(_ context.Context, environment string) (string, bool, error) {
		if environment == "elsewhere" {
			return "octo-elsewhere", true, nil
		}
		return "", false, nil
	})

	cases := map[string]string{
		"":          testNamespace,
		"staging":   "octo-staging",
		"elsewhere": "octo-elsewhere",
		"unknown":   unresolvedNamespace,
	}
	for environment, want := range cases {
		if got := c.ns(current.With(ctx, environment, true)); got != want {
			t.Errorf("ns(%q) = %q, want %q", environment, got, want)
		}
	}
	if ns, ok := c.environmentNamespaces.get("elsewhere"); !ok || ns != "octo-elsewhere" {
		t.Error("a resolved environment was not registered")
	}
	c.ForgetEnvironment("staging")
	if got := c.ns(current.With(ctx, "staging", true)); got != unresolvedNamespace {
		t.Errorf("after forgetting, ns(staging) = %q", got)
	}
}

// The first read of an environment another replica created resolves and starts
// watching it, under the informers' lock; doing that while holding the lock to
// read the cache would hang the request for good.
func TestCacheResolvesAnEnvironmentWithoutDeadlocking(t *testing.T) {
	c := environmentClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.StartInformers(ctx, func(string) {})
	c.ResolveEnvironmentsWith(func(context.Context, string) (string, bool, error) {
		return "octo-elsewhere", true, nil
	})

	done := make(chan struct{})
	go func() {
		c.cache(current.With(ctx, "elsewhere", true))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reading the cache for a newly resolved environment deadlocked")
	}
}

// A deploy in an environment lands in its namespace and nowhere else.
func TestApplyLandsInTheEnvironmentsNamespace(t *testing.T) {
	c := environmentClient()
	c.RegisterEnvironment("staging", "octo-staging")
	ctx := current.With(context.Background(), "staging", true)
	if err := c.Apply(ctx, Spec{ID: "dep-1", IntegrationID: "int-1", Definition: "flows: []"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, err := c.clientset.AppsV1().Deployments("octo-staging").Get(ctx, resourceName("dep-1"), metav1.GetOptions{}); err != nil {
		t.Errorf("the deployment is not in octo-staging: %v", err)
	}
	if _, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, resourceName("dep-1"), metav1.GetOptions{}); err == nil {
		t.Error("the deployment also landed in the release namespace")
	}
}

func TestTheReleaseNamespaceAndEnvironmentsShareTheReleaseLabel(t *testing.T) {
	ctx := context.Background()
	release := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}
	c := environmentClient(release)
	if err := c.LabelReleaseNamespace(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureNamespace(ctx, "staging", "octo-staging"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{testNamespace, "octo-staging"} {
		ns, err := c.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if ns.Labels[labelRelease] != testNamespace {
			t.Errorf("%s: %s = %q, want %q", name, labelRelease, ns.Labels[labelRelease], testNamespace)
		}
	}
}
