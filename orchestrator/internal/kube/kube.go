// Package kube wraps the Kubernetes API access the orchestrator needs to run an
// integration as its own workload. Each deployment maps to three resources in
// the target namespace — a ConfigMap carrying the integration YAML, a Deployment
// running the generic octo-runtime image, and a ClusterIP Service — all named
// deterministically from the deployment id and labelled so they can be resolved
// without persisting their names.
package kube

import (
	"context"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Status values cached for a deployment. They are intentionally coarse — enough
// to drive the UI badge — and computed from the live Deployment/pod state.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusFailed  = "failed"
)

const (
	// configMountPath is where the integration YAML is mounted; octo loads every
	// .yaml/.yml in this directory (matches the image's default --config).
	configMountPath = "/etc/octo/integrations"
	// configFileName is the single key/file written into the ConfigMap.
	configFileName = "integration.yaml"
	// runtimePort is the conventional port the Service targets. It is a
	// placeholder until real per-integration port wiring + ingress arrive; the
	// Service simply has no endpoints if the integration does not bind it.
	runtimePort = 8080

	labelManagedBy     = "app.kubernetes.io/managed-by"
	labelDeploymentID  = "octo.dev/deployment-id"
	labelIntegrationID = "octo.dev/integration-id"
	managedByValue     = "orchestrator"
)

// Spec describes the workload to create for one deployment.
type Spec struct {
	ID            string // deployment uuid; drives resource names and labels
	IntegrationID string // owning integration uuid (label only)
	Definition    string // runtime-loadable integration YAML
}

// Client wraps a Kubernetes clientset scoped to one namespace and runtime image.
type Client struct {
	clientset    kubernetes.Interface
	namespace    string
	runtimeImage string
}

// New builds a Client from the in-cluster config. It returns an error when the
// orchestrator is not running inside a cluster (e.g. local `go run`), letting the
// caller disable deployment features rather than crash.
func New(namespace, runtimeImage string) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kube: in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube: clientset: %w", err)
	}
	return &Client{clientset: cs, namespace: namespace, runtimeImage: runtimeImage}, nil
}

// Namespace returns the namespace the client operates in.
func (c *Client) Namespace() string { return c.namespace }

// resourceName is the deterministic name shared by a deployment's resources.
// "octo-dep-" + a uuid stays within the 63-char DNS-1123 label limit.
func resourceName(deploymentID string) string { return "octo-dep-" + deploymentID }

func (c *Client) labels(spec Spec) map[string]string {
	return map[string]string{
		labelManagedBy:     managedByValue,
		labelDeploymentID:  spec.ID,
		labelIntegrationID: spec.IntegrationID,
	}
}

// selector matches all resources for one deployment by its id label.
func selector(deploymentID string) string {
	return labelDeploymentID + "=" + deploymentID
}

// Apply creates the ConfigMap, Deployment and Service for spec. It is not
// idempotent: a deployment id is single-use, so AlreadyExists is surfaced as an
// error for the caller to handle (and roll back).
func (c *Client) Apply(ctx context.Context, spec Spec) error {
	name := resourceName(spec.ID)
	labels := c.labels(spec)
	cms := c.clientset.CoreV1().ConfigMaps(c.namespace)
	if _, err := cms.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Data:       map[string]string{configFileName: spec.Definition},
	}, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("kube: create configmap: %w", err)
	}

	deps := c.clientset.AppsV1().Deployments(c.namespace)
	if _, err := deps.Create(ctx, c.deployment(name, labels), metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("kube: create deployment: %w", err)
	}

	svcs := c.clientset.CoreV1().Services(c.namespace)
	if _, err := svcs.Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{labelDeploymentID: spec.ID},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       runtimePort,
				TargetPort: intstr.FromInt(runtimePort),
			}},
		},
	}, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("kube: create service: %w", err)
	}
	return nil
}

// deployment builds the Deployment object: one replica of the runtime image with
// the integration ConfigMap mounted read-only at the config path.
func (c *Client) deployment(name string, labels map[string]string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{labelDeploymentID: labels[labelDeploymentID]},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:            "runtime",
						Image:           c.runtimeImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "integration",
							MountPath: configMountPath,
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "integration",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: name},
							},
						},
					}},
				},
			},
		},
	}
}

// Status reports a coarse lifecycle status for a deployment, computed from the
// live Deployment and its pods. A missing Deployment reads as failed: the row
// exists but its workload is gone.
func (c *Client) Status(ctx context.Context, deploymentID string) (string, error) {
	name := resourceName(deploymentID)
	dep, err := c.clientset.AppsV1().Deployments(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return StatusFailed, nil
		}
		return "", fmt.Errorf("kube: get deployment: %w", err)
	}
	if dep.Status.ReadyReplicas >= 1 {
		return StatusRunning, nil
	}
	// Not ready yet: surface a terminal pull/crash failure quickly rather than
	// reporting pending forever.
	pods, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx,
		metav1.ListOptions{LabelSelector: selector(deploymentID)})
	if err != nil {
		return "", fmt.Errorf("kube: list pods: %w", err)
	}
	for i := range pods.Items {
		for _, cs := range pods.Items[i].Status.ContainerStatuses {
			if w := cs.State.Waiting; w != nil && isTerminalWaiting(w.Reason) {
				return StatusFailed, nil
			}
		}
	}
	return StatusPending, nil
}

// isTerminalWaiting reports whether a container's waiting reason means the pod
// will not recover on its own.
func isTerminalWaiting(reason string) bool {
	switch reason {
	case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerError", "CreateContainerConfigError":
		return true
	default:
		return false
	}
}

// Delete removes the Deployment, Service and ConfigMap for a deployment. Missing
// resources are ignored so undeploy is safe to retry.
func (c *Client) Delete(ctx context.Context, deploymentID string) error {
	name := resourceName(deploymentID)
	del := metav1.DeleteOptions{}
	var errs []error
	if err := c.clientset.AppsV1().Deployments(c.namespace).Delete(ctx, name, del); ignoreNotFound(err) != nil {
		errs = append(errs, fmt.Errorf("delete deployment: %w", err))
	}
	if err := c.clientset.CoreV1().Services(c.namespace).Delete(ctx, name, del); ignoreNotFound(err) != nil {
		errs = append(errs, fmt.Errorf("delete service: %w", err))
	}
	if err := c.clientset.CoreV1().ConfigMaps(c.namespace).Delete(ctx, name, del); ignoreNotFound(err) != nil {
		errs = append(errs, fmt.Errorf("delete configmap: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("kube: delete %s: %w", name, errors.Join(errs...))
	}
	return nil
}

// ignoreNotFound returns nil for a NotFound error, passing anything else through.
func ignoreNotFound(err error) error {
	if err == nil || apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
