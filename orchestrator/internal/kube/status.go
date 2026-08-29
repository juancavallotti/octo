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
	"k8s.io/apimachinery/pkg/selection"
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

// cacheSyncPoll is how often WaitForCacheSync checks. It runs once at startup, so
// the granularity is about not busy-waiting rather than about latency.
const cacheSyncPoll = 200 * time.Millisecond

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
	// RuntimeImage is the octo runtime image the pods are running — the image the
	// containers actually report, so a deployment mid-rollout (or one left behind
	// on an older image) says what is really serving rather than what the spec
	// asks for. Falls back to the spec's image while no pod has reported one.
	RuntimeImage string
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

// runtimeContainer is the name given to the octo runtime container in every
// deployment's pod spec (see deploy.go); pods carry other containers only in the
// dev-run path, which is not what a deployment reports.
const runtimeContainer = "runtime"

// runtimeImage reports the image the deployment's runtime container is running.
// A container status is preferred over the pod spec because that is the image the
// kubelet actually pulled: during a rollout, or while a new image fails to pull,
// the spec already names the new one while the old one is still serving. The
// spec is the fallback for a deployment whose pods have not reported yet.
//
// A serving pod is asked first, and that ordering is the point rather than a
// nicety. A rollout has both generations present at once, and the new pod — the
// one that is unready, crash-looping, or stuck on a pull — is as likely to be
// first in the list as the old one that is answering traffic. Reporting its image
// would name a runtime that is not serving anything, on precisely the deployment
// somebody is looking at because it is misbehaving.
func runtimeImage(dep *appsv1.Deployment, pods []*corev1.Pod) string {
	if image := containerImage(pods, func(p *corev1.Pod) bool {
		return p.Status.Phase == corev1.PodRunning && podReady(p)
	}); image != "" {
		return image
	}
	// Nothing is serving: any pod that has reported still says more than the spec,
	// which describes what was asked for rather than what happened.
	if image := containerImage(pods, func(*corev1.Pod) bool { return true }); image != "" {
		return image
	}
	for _, c := range dep.Spec.Template.Spec.Containers {
		if c.Name == runtimeContainer {
			return c.Image
		}
	}
	return ""
}

// containerImage is the runtime container's image on the first pod that satisfies
// want, or "" when none does.
func containerImage(pods []*corev1.Pod, want func(*corev1.Pod) bool) string {
	for _, p := range pods {
		if !want(p) {
			continue
		}
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Name == runtimeContainer && cs.Image != "" {
				return cs.Image
			}
		}
	}
	return ""
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
	st.RuntimeImage = runtimeImage(dep, pods)
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

// WaitForCacheSync blocks until the informer caches have loaded, or ctx ends.
//
// It exists for callers that must not act on an empty cache — the reconciler
// decides what to delete, and an unloaded cache and an empty cluster look
// identical. Everything else here treats an unsynced cache as a reason to read
// the API server directly, which is fine when the question is about one named
// object and not fine when it is "what is there".
//
// False means the caches never synced: informers were not started, or ctx ended
// first. Both mean the same thing to a caller — do not act.
func (c *Client) WaitForCacheSync(ctx context.Context) bool {
	if c.synced == nil {
		return false
	}
	// A short poll rather than cache.WaitForCacheSync, which takes a stop channel
	// and would need one derived from ctx. The wait happens once, at startup, and
	// half a second of latency on it costs nothing.
	ticker := time.NewTicker(cacheSyncPoll)
	defer ticker.Stop()
	for {
		if c.synced() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

// notifyIntegration extracts the integration-id label from a changed object (or
// the wrapped object of a delete tombstone) and reports it.
//
// Dev-run workloads are skipped, and that guard is load-bearing rather than tidy.
// A dev run carries the same managed-by label the informers filter on AND an
// integration id, so without it every dev-run pod transition would publish a
// *deployment* snapshot for that integration — telling anyone watching
// internal.deployments.{id} that a deployment changed when none did, or that one
// exists when none does. The dev-run id is what distinguishes the two kinds of
// workload; dev-run status has its own read path (ListDevRuns).
func notifyIntegration(obj any, onChange func(string)) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	m, err := meta.Accessor(obj)
	if err != nil {
		return
	}
	if m.GetLabels()[labelDevRunID] != "" {
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

// Reachable reports whether the Kubernetes API answers, or why not.
//
// It asks the API server for its version rather than listing anything in the
// namespace: the version endpoint needs no RBAC beyond what any authenticated
// client has, so a failure here means the connection is broken rather than that
// this service account is missing a permission — which is a different problem
// with a different fix, and one the admin page must not confuse it with.
func (c *Client) Reachable(ctx context.Context) error {
	// The discovery client takes no context, so the cancellation the caller set is
	// honoured by racing it rather than by being passed down. Left unraced, a
	// wedged API server would hold the health page past its own deadline.
	done := make(chan error, 1)
	go func() {
		_, err := c.clientset.Discovery().ServerVersion()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("kube: server version: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DeploymentIDs returns the id of every deployment this orchestrator has a
// workload for, and whether the answer can be trusted.
//
// The selector is the load-bearing part and it has three clauses, not one.
// Managed by us, carrying a deployment id, and — the one that matters —
// explicitly NOT carrying a dev-run id. A dev run wears the same managed-by
// label and an integration id, so a selector that stopped at the first two would
// report every running dev run as an orphaned deployment, and the reconciler
// would delete them. It is the inverse of devRunSelector, deliberately spelled
// out here rather than derived from it, because the two are read together and a
// reader has to be able to see that they partition the namespace.
//
// The second return says the list is authoritative, which is a stronger claim
// than "it came back". It is false only when neither source could give a complete
// answer; a caller deciding what to delete must not act on a list without it,
// because an incomplete listing and an empty cluster are the same empty map.
//
// A cache that has not synced does not make the answer untrustworthy — it makes
// this read fall back to the API server, which is authoritative by definition.
// What the fallback costs is a full list rather than a cache read, and this runs
// once every few minutes.
func (c *Client) DeploymentIDs(ctx context.Context) (map[string]bool, bool, error) {
	sel, err := managedDeploymentSelector()
	if err != nil {
		return nil, false, err
	}

	var deployments []*appsv1.Deployment
	if c.synced != nil && c.synced() {
		deployments, err = c.depLister.List(sel)
		if err != nil {
			return nil, false, fmt.Errorf("kube: lister list deployments: %w", err)
		}
	} else {
		// A direct read rather than a refusal. The cache is unsynced for a few
		// seconds after start and forever when informers were never started at all
		// (a test, a one-shot); in both cases the API server can still answer, and
		// answering from it is more useful than reporting the whole namespace
		// unknowable.
		list, lerr := c.clientset.AppsV1().Deployments(c.namespace).List(ctx,
			metav1.ListOptions{LabelSelector: sel.String()})
		if lerr != nil {
			return nil, false, fmt.Errorf("kube: list deployments: %w", lerr)
		}
		deployments = make([]*appsv1.Deployment, len(list.Items))
		for i := range list.Items {
			deployments[i] = &list.Items[i]
		}
	}

	out := make(map[string]bool, len(deployments))
	for _, dep := range deployments {
		if id := dep.Labels[labelDeploymentID]; id != "" {
			out[id] = true
		}
	}
	return out, true, nil
}

// DeploymentExists reports whether the workload for a deployment id is there,
// asked by name rather than by label.
//
// It is the second opinion the reconciler takes before deleting a row, and the
// difference between the two questions is the point. DeploymentIDs asks the
// cluster "what do you have", which it answers from labels — metadata that any
// principal with cluster access can edit, and that an admission controller can
// rewrite. This asks about one object by the name derived from the row's own id,
// which is the identity every other read here uses and the one thing about a
// workload that cannot drift.
//
// Without it, a stripped label makes a live deployment invisible to the sweep,
// which then deletes its row — irreversibly, taking the settings and env bindings
// with it — while the workload keeps running and can never be collected either,
// because it does not match the selector any more.
func (c *Client) DeploymentExists(ctx context.Context, deploymentID string) (bool, error) {
	// Deliberately not the lister: this is the confirmation step for a destructive
	// action, and a cache is a copy of what was true a moment ago.
	_, err := c.clientset.AppsV1().Deployments(c.namespace).Get(ctx,
		resourceName(deploymentID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("kube: get deployment: %w", err)
	}
	return true, nil
}

// managedDeploymentSelector matches orchestrator-managed deployment workloads and
// nothing else. See DeploymentIDs for why each clause is there.
func managedDeploymentSelector() (labels.Selector, error) {
	hasDeployment, err := labels.NewRequirement(labelDeploymentID, selection.Exists, nil)
	if err != nil {
		return nil, fmt.Errorf("kube: deployment selector: %w", err)
	}
	notDevRun, err := labels.NewRequirement(labelDevRunID, selection.DoesNotExist, nil)
	if err != nil {
		return nil, fmt.Errorf("kube: deployment selector: %w", err)
	}
	return labels.SelectorFromSet(labels.Set{labelManagedBy: managedByValue}).
		Add(*hasDeployment, *notDevRun), nil
}
