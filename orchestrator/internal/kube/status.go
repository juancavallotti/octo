package kube

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// Status values cached for a deployment. They are intentionally coarse — enough
// to drive the UI badge — and computed from the live Deployment/pod state.
const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusFailed  = "failed"
)

// informerResync is the periodic full relist interval; it backstops any missed
// watch events without making the cache stale in normal operation.
const informerResync = 5 * time.Minute

// PodStatus is the live state of one runtime pod.
type PodStatus struct {
	Name     string // pod name
	Phase    string // Pending/Running/Succeeded/Failed/Unknown
	Ready    bool   // the pod's Ready condition is true
	Restarts int32  // total container restarts across the pod
}

// Status is the live status of a deployment, computed from the Deployment and its
// pods. Phase is the coarse value cached in the database; the rest is detail for
// the UI and is not persisted.
type Status struct {
	Phase           string      // pending|running|failed
	DesiredReplicas int32       // spec replica count
	ReadyReplicas   int32       // ready replica count
	Reason          string      // terminal failure reason (e.g. ImagePullBackOff), when failed
	CreatedAt       time.Time   // Deployment creation timestamp (workload age)
	Pods            []PodStatus // per-pod detail
}

// Status reports the live status for a deployment, computed from the Deployment
// and its pods. A missing Deployment reads as failed: the row exists but its
// workload is gone. Reads come from the informer caches when they are synced,
// falling back to direct API calls otherwise.
func (c *Client) Status(ctx context.Context, deploymentID string) (Status, error) {
	dep, pods, err := c.fetchWorkload(ctx, deploymentID)
	if err != nil {
		return Status{}, err
	}
	if dep == nil {
		return Status{Phase: StatusFailed}, nil
	}
	return computeStatus(dep, pods), nil
}

// fetchWorkload returns the Deployment and its pods for a deployment id, or a nil
// Deployment when it does not exist. It prefers the informer cache (when synced)
// and falls back to direct API reads.
func (c *Client) fetchWorkload(ctx context.Context, deploymentID string) (*appsv1.Deployment, []*corev1.Pod, error) {
	name := resourceName(deploymentID)
	if c.synced != nil && c.synced() {
		dep, err := c.depLister.Get(name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, nil
			}
			return nil, nil, fmt.Errorf("kube: lister get deployment: %w", err)
		}
		pods, err := c.podLister.List(labels.Set{labelDeploymentID: deploymentID}.AsSelector())
		if err != nil {
			return nil, nil, fmt.Errorf("kube: lister list pods: %w", err)
		}
		return dep, pods, nil
	}

	dep, err := c.clientset.AppsV1().Deployments(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("kube: get deployment: %w", err)
	}
	list, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx,
		metav1.ListOptions{LabelSelector: selector(deploymentID)})
	if err != nil {
		return nil, nil, fmt.Errorf("kube: list pods: %w", err)
	}
	pods := make([]*corev1.Pod, len(list.Items))
	for i := range list.Items {
		pods[i] = &list.Items[i]
	}
	return dep, pods, nil
}

// computeStatus derives a Status from a Deployment and its pods. Pure (no I/O) so
// it serves both the cache and direct-read paths identically.
func computeStatus(dep *appsv1.Deployment, pods []*corev1.Pod) Status {
	st := Status{
		Phase:         StatusPending,
		ReadyReplicas: dep.Status.ReadyReplicas,
		CreatedAt:     dep.CreationTimestamp.Time,
	}
	if dep.Spec.Replicas != nil {
		st.DesiredReplicas = *dep.Spec.Replicas
	}
	for _, p := range pods {
		ps := PodStatus{Name: p.Name, Phase: string(p.Status.Phase), Ready: podReady(p)}
		for _, cs := range p.Status.ContainerStatuses {
			ps.Restarts += cs.RestartCount
			if w := cs.State.Waiting; w != nil && isTerminalWaiting(w.Reason) && st.Reason == "" {
				st.Reason = w.Reason
				if w.Message != "" {
					st.Reason = w.Reason + ": " + w.Message
				}
			}
		}
		st.Pods = append(st.Pods, ps)
	}
	switch {
	case dep.Status.ReadyReplicas >= 1:
		st.Phase = StatusRunning
	case st.Reason != "":
		// A terminal pull/crash failure: surface it rather than reporting pending
		// forever.
		st.Phase = StatusFailed
	}
	return st
}

// StartInformers begins watching the orchestrator-managed Deployments and Pods in
// the namespace and invokes onChange(integrationID) whenever one changes, so the
// caller can push live updates. It also wires the lister-backed read path used by
// Status. The informers run until ctx is cancelled.
func (c *Client) StartInformers(ctx context.Context, onChange func(integrationID string)) {
	factory := informers.NewSharedInformerFactoryWithOptions(
		c.clientset, informerResync,
		informers.WithNamespace(c.namespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = labelManagedBy + "=" + managedByValue
		}),
	)
	depInformer := factory.Apps().V1().Deployments()
	podInformer := factory.Core().V1().Pods()
	c.depLister = depInformer.Lister().Deployments(c.namespace)
	c.podLister = podInformer.Lister().Pods(c.namespace)

	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { notifyIntegration(obj, onChange) },
		UpdateFunc: func(_, obj any) { notifyIntegration(obj, onChange) },
		DeleteFunc: func(obj any) { notifyIntegration(obj, onChange) },
	}
	// AddEventHandler can only fail before the informer starts; ignore the
	// registration handle since handlers live for the informer's lifetime.
	_, _ = depInformer.Informer().AddEventHandler(handler)
	_, _ = podInformer.Informer().AddEventHandler(handler)

	factory.Start(ctx.Done())
	c.synced = func() bool {
		return depInformer.Informer().HasSynced() && podInformer.Informer().HasSynced()
	}
}

// notifyIntegration extracts the integration-id label from a changed object (or
// the wrapped object of a delete tombstone) and reports it.
func notifyIntegration(obj any, onChange func(string)) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	m, err := meta.Accessor(obj)
	if err != nil {
		return
	}
	if id := m.GetLabels()[labelIntegrationID]; id != "" {
		onChange(id)
	}
}

// podReady reports whether the pod's Ready condition is true.
func podReady(p *corev1.Pod) bool {
	for _, cond := range p.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
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
