package kube

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// labelEnvironment marks a namespace as an environment's, naming which one. It
// is what DeleteNamespace checks before deleting anything, so a namespace this
// orchestrator did not create is never one it removes.
const labelEnvironment = "octo.dev/environment"

// environmentBinding names the RoleBindings provisioning writes into an
// environment's namespace. One name for both, since each lives in its own
// namespace and they are told apart by subject.
const (
	orchestratorBinding = "octo-orchestrator"
	runtimeBinding      = "octo-runtime"
)

// ErrNamespaceTaken is returned when the namespace an environment asks for
// already exists and is not that environment's: adopting it would hand octo a
// namespace somebody else owns, and deleting the environment later would
// delete their namespace.
var ErrNamespaceTaken = errors.New("kube: namespace exists and is not this environment's")

// EnvironmentRBAC is what an environment's namespace is wired with. The chart
// creates the two ClusterRoles and grants the orchestrator `bind` on exactly
// those, so provisioning hands out nothing beyond what the release namespace
// already has.
type EnvironmentRBAC struct {
	// OrchestratorServiceAccount is this orchestrator's own account, in Namespace.
	OrchestratorServiceAccount string
	// WorkloadClusterRole holds the orchestrator's namespaced rules, bound to it
	// in each environment's namespace.
	WorkloadClusterRole string
	// RuntimeClusterRole holds the runtime pods' Lease rules, bound to the
	// runtime account in each environment's namespace.
	RuntimeClusterRole string
}

// configured reports whether every part is named. All three are needed: without
// the workload role the orchestrator cannot deploy into the namespace it just
// created, and without the runtime role a pod there cannot elect a leader.
func (e EnvironmentRBAC) configured() bool {
	return e.OrchestratorServiceAccount != "" && e.WorkloadClusterRole != "" && e.RuntimeClusterRole != ""
}

// EnvironmentsEnabled reports whether environment namespaces can be provisioned.
func (c *Client) EnvironmentsEnabled() bool { return c.environments.configured() }

// EnsureNamespace provisions namespace for environment: the namespace itself,
// the orchestrator's RoleBinding into it, the runtime ServiceAccount and its
// RoleBinding, and copies of the image pull secrets runtime pods name. Each step
// tolerates finding its object already there, so a provisioning interrupted
// half way is finished by running it again.
//
// The orchestrator binding is written before anything else inside the
// namespace, because it is what grants the rights to write the rest.
func (c *Client) EnsureNamespace(ctx context.Context, environment, namespace string) error {
	if err := c.createNamespace(ctx, environment, namespace); err != nil {
		return err
	}
	rbac := c.clientset.RbacV1().RoleBindings(namespace)
	if err := createIgnoringExisting(func() error {
		_, err := rbac.Create(ctx, c.roleBinding(orchestratorBinding, c.environments.WorkloadClusterRole,
			c.environments.OrchestratorServiceAccount, c.namespace), metav1.CreateOptions{})
		return err
	}); err != nil {
		return fmt.Errorf("kube: bind orchestrator in %s: %w", namespace, err)
	}

	if sa := c.runtimeServices.ServiceAccount; sa != "" {
		if err := createIgnoringExisting(func() error {
			_, err := c.clientset.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: sa, Labels: c.environmentLabels(environment)},
			}, metav1.CreateOptions{})
			return err
		}); err != nil {
			return fmt.Errorf("kube: runtime service account in %s: %w", namespace, err)
		}
		if err := createIgnoringExisting(func() error {
			_, err := rbac.Create(ctx, c.roleBinding(runtimeBinding, c.environments.RuntimeClusterRole,
				sa, namespace), metav1.CreateOptions{})
			return err
		}); err != nil {
			return fmt.Errorf("kube: bind runtime in %s: %w", namespace, err)
		}
	}

	for _, name := range c.imagePullSecrets {
		if err := c.copySecret(ctx, name, namespace, environment); err != nil {
			return err
		}
	}
	return nil
}

// createNamespace creates namespace labelled for environment, or confirms an
// existing one already carries that label.
func (c *Client) createNamespace(ctx context.Context, environment, namespace string) error {
	namespaces := c.clientset.CoreV1().Namespaces()
	_, err := namespaces.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace, Labels: c.environmentLabels(environment)},
	}, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("kube: create namespace %s: %w", namespace, err)
	}
	existing, err := namespaces.Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("kube: get namespace %s: %w", namespace, err)
	}
	if existing.Labels[labelEnvironment] != environment {
		return ErrNamespaceTaken
	}
	return nil
}

// copySecret copies one Secret from the release namespace into namespace,
// leaving a copy that is already there alone. A pull secret the release
// namespace does not have is skipped rather than fatal: the pods it was meant
// for would fail the same way in the release namespace.
func (c *Client) copySecret(ctx context.Context, name, namespace, environment string) error {
	src, err := c.clientset.CoreV1().Secrets(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("kube: read secret %s: %w", name, err)
	}
	if err := createIgnoringExisting(func() error {
		_, err := c.clientset.CoreV1().Secrets(namespace).Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: c.environmentLabels(environment)},
			Type:       src.Type,
			Data:       src.Data,
		}, metav1.CreateOptions{})
		return err
	}); err != nil {
		return fmt.Errorf("kube: copy secret %s into %s: %w", name, namespace, err)
	}
	return nil
}

// NamespaceInUse reports whether namespace still holds anything this
// orchestrator manages: a Deployment, a test-run Job, or a pod such as a dev
// run. Deleting an environment that does would delete running workloads with it.
func (c *Client) NamespaceInUse(ctx context.Context, namespace string) (bool, error) {
	opts := metav1.ListOptions{LabelSelector: labelManagedBy + "=" + managedByValue, Limit: 1}
	deployments, err := c.clientset.AppsV1().Deployments(namespace).List(ctx, opts)
	if err != nil {
		return false, fmt.Errorf("kube: list deployments in %s: %w", namespace, err)
	}
	if len(deployments.Items) > 0 {
		return true, nil
	}
	jobs, err := c.clientset.BatchV1().Jobs(namespace).List(ctx, opts)
	if err != nil {
		return false, fmt.Errorf("kube: list jobs in %s: %w", namespace, err)
	}
	if len(jobs.Items) > 0 {
		return true, nil
	}
	pods, err := c.clientset.CoreV1().Pods(namespace).List(ctx, opts)
	if err != nil {
		return false, fmt.Errorf("kube: list pods in %s: %w", namespace, err)
	}
	return len(pods.Items) > 0, nil
}

// DeleteNamespace deletes namespace if it is environment's. A namespace that is
// already gone is not an error, and one carrying another label is left alone
// and reported: it is somebody else's.
func (c *Client) DeleteNamespace(ctx context.Context, environment, namespace string) error {
	namespaces := c.clientset.CoreV1().Namespaces()
	existing, err := namespaces.Get(ctx, namespace, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("kube: get namespace %s: %w", namespace, err)
	}
	if existing.Labels[labelEnvironment] != environment {
		return ErrNamespaceTaken
	}
	if err := namespaces.Delete(ctx, namespace, metav1.DeleteOptions{}); ignoreNotFound(err) != nil {
		return fmt.Errorf("kube: delete namespace %s: %w", namespace, err)
	}
	return nil
}

func (c *Client) environmentLabels(environment string) map[string]string {
	return map[string]string{labelManagedBy: managedByValue, labelEnvironment: environment}
}

// roleBinding binds the ServiceAccount account (in accountNamespace) to the
// ClusterRole clusterRole, within whichever namespace the binding is created in.
func (c *Client) roleBinding(name, clusterRole, account, accountNamespace string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{labelManagedBy: managedByValue}},
		Subjects: []rbacv1.Subject{{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      account,
			Namespace: accountNamespace,
		}},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: clusterRole},
	}
}

// createIgnoringExisting runs create and treats AlreadyExists as success.
func createIgnoringExisting(create func() error) error {
	if err := create(); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
