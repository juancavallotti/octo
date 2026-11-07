package kube

import (
	"context"
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

	orch, err := c.clientset.RbacV1().RoleBindings("octo-staging").Get(ctx, c.environments.WorkloadClusterRole, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("orchestrator binding: %v", err)
	}
	if orch.RoleRef.Name != "octo-orchestrator-environment" ||
		orch.Subjects[0].Name != "octo-orchestrator" || orch.Subjects[0].Namespace != testNamespace {
		t.Errorf("orchestrator binding = %+v", orch)
	}
	rt, err := c.clientset.RbacV1().RoleBindings("octo-staging").Get(ctx, c.environments.RuntimeClusterRole, metav1.GetOptions{})
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
	c := environmentClient(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "octo-staging"}}, unmanaged)

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

// A namespace that is gone or terminating protects nothing, and the orchestrator
// may no longer list in it; asking must not stop the environment's delete.
func TestNamespaceInUseIsFalseForAGoneOrTerminatingNamespace(t *testing.T) {
	ctx := context.Background()
	c := environmentClient()
	c.clientset.(*fake.Clientset).PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{}, "", errors.New("no rights here"))
	})
	if used, err := c.NamespaceInUse(ctx, "octo-gone"); err != nil || used {
		t.Errorf("a missing namespace: used = %v, err = %v; want not in use", used, err)
	}

	now := metav1.Now()
	terminating := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "octo-ending", DeletionTimestamp: &now}}
	c = environmentClient(terminating)
	c.clientset.(*fake.Clientset).PrependReactor("list", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{}, "", errors.New("no rights here"))
	})
	if used, err := c.NamespaceInUse(ctx, "octo-ending"); err != nil || used {
		t.Errorf("a terminating namespace: used = %v, err = %v; want not in use", used, err)
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

// A role the chart renames gets a binding of its own: a binding's role cannot
// change in place, and the orchestrator may create bindings but not delete them.
func TestARenamedRoleGetsItsOwnBinding(t *testing.T) {
	ctx := context.Background()
	c := environmentClient()
	if err := c.EnsureNamespace(ctx, "staging", "octo-staging"); err != nil {
		t.Fatal(err)
	}
	c.environments.WorkloadClusterRole = "octo-orchestrator-renamed-workload"
	if err := c.EnsureNamespace(ctx, "staging", "octo-staging"); err != nil {
		t.Fatal(err)
	}
	got, err := c.clientset.RbacV1().RoleBindings("octo-staging").Get(ctx, "octo-orchestrator-renamed-workload", metav1.GetOptions{})
	if err != nil || got.RoleRef.Name != "octo-orchestrator-renamed-workload" {
		t.Errorf("binding = %v, %v; want one granting the renamed role", got, err)
	}
}
