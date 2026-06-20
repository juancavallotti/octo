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
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
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
	// runtimePort is the default port the Service/Ingress target when a deployment
	// declares no HTTP_PORT (Spec.Port == 0). An integration that declares
	// HTTP_PORT overrides it; the Service simply has no endpoints if the runtime
	// does not bind the resolved port.
	runtimePort = 8080

	labelManagedBy     = "app.kubernetes.io/managed-by"
	labelDeploymentID  = "octo.dev/deployment-id"
	labelIntegrationID = "octo.dev/integration-id"
	managedByValue     = "orchestrator"
)

// Spec describes the workload to create for one deployment.
type Spec struct {
	ID            string            // deployment uuid; drives resource names and labels
	IntegrationID string            // owning integration uuid (label + internal Service selector)
	Definition    string            // runtime-loadable integration YAML
	Replicas      int32             // desired replica count; <1 is treated as 1
	Slug          string            // unique slug naming this deployment's internal Service ("" = none)
	Port          int               // runtime HTTP port (from HTTP_PORT); 0 means no HTTP source (no Service)
	Env           map[string]string // env vars supplied to the runtime container (e.g. HTTP_HOST/HTTP_PORT)
	Expose        bool              // when true, also publish an external Ingress
	Subdomain     string            // external host label; the Ingress host is {Subdomain}.{baseDomain}
}

// port returns the resolved runtime port, defaulting to runtimePort when unset.
func (s Spec) port() int32 {
	if s.Port > 0 {
		return int32(s.Port)
	}
	return runtimePort
}

// networked reports whether the deployment serves HTTP on a port — i.e. its
// integration declared HTTP_PORT. Only networked deployments get Services (a
// per-deployment one, a stable internal one) and the option of an Ingress; a
// deployment with no HTTP source (a timer, a scheduled job) runs as a bare
// workload with no Service at all.
func (s Spec) networked() bool { return s.Port > 0 }

// informerResync is the periodic full relist interval; it backstops any missed
// watch events without making the cache stale in normal operation.
const informerResync = 5 * time.Minute

// Client wraps a Kubernetes clientset scoped to one namespace and runtime image.
type Client struct {
	clientset     kubernetes.Interface
	namespace     string
	runtimeImage  string
	baseDomain    string // parent domain for external endpoints ("" = disabled)
	clusterIssuer string // cert-manager ClusterIssuer for external TLS

	// Informer-backed read path, populated by StartInformers. When synced reports
	// true, Status reads from these caches instead of hitting the API server.
	depLister corelisterDeployments
	podLister corelisters.PodNamespaceLister
	synced    func() bool
}

// corelisterDeployments aliases the namespaced Deployment lister for brevity.
type corelisterDeployments = appslisters.DeploymentNamespaceLister

// New builds a Client from the in-cluster config. It returns an error when the
// orchestrator is not running inside a cluster (e.g. local `go run`), letting the
// caller disable deployment features rather than crash. baseDomain may be empty,
// which disables external endpoints (Apply then ignores Spec.Expose).
func New(namespace, runtimeImage, baseDomain, clusterIssuer string) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kube: in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube: clientset: %w", err)
	}
	return &Client{
		clientset:     cs,
		namespace:     namespace,
		runtimeImage:  runtimeImage,
		baseDomain:    baseDomain,
		clusterIssuer: clusterIssuer,
	}, nil
}

// ExternalEnabled reports whether external endpoints can be published (a base
// domain is configured).
func (c *Client) ExternalEnabled() bool { return c.baseDomain != "" }

// ExternalHost is the fully-qualified host for an external subdomain, or "" when
// external endpoints are disabled or the subdomain is empty.
func (c *Client) ExternalHost(subdomain string) string {
	if c.baseDomain == "" || subdomain == "" {
		return ""
	}
	return subdomain + "." + c.baseDomain
}

// ExternalURL is the public https URL for an external subdomain, or "" when not
// applicable.
func (c *Client) ExternalURL(subdomain string) string {
	host := c.ExternalHost(subdomain)
	if host == "" {
		return ""
	}
	return "https://" + host
}

// Namespace returns the namespace the client operates in.
func (c *Client) Namespace() string { return c.namespace }

// resourceName is the deterministic name shared by a deployment's resources.
// "octo-dep-" + a uuid stays within the 63-char DNS-1123 label limit.
func resourceName(deploymentID string) string { return "octo-dep-" + deploymentID }

// internalServiceName is the stable, per-deployment Service name other flows
// address to reach a deployment by a constant name. The slug is unique per
// deployment; "octo-int-" + a slug (≤54 chars; the caller bounds it) stays within
// the 63-char DNS-1123 label limit.
func internalServiceName(slug string) string { return "octo-int-" + slug }

// InternalURL is the in-cluster address of the stable internal Service for slug,
// on the deployment's runtime port (port <1 falls back to runtimePort).
func (c *Client) InternalURL(slug string, port int) string {
	if slug == "" {
		return ""
	}
	p := port
	if p < 1 {
		p = runtimePort
	}
	return fmt.Sprintf("http://%s.%s:%d", internalServiceName(slug), c.namespace, p)
}

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
	if _, err := deps.Create(ctx, c.deployment(name, labels, spec), metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("kube: create deployment: %w", err)
	}

	// A deployment with no HTTP source listens on nothing, so it needs no Service,
	// no internal endpoint and no Ingress: the workload alone is the whole deploy.
	if !spec.networked() {
		return nil
	}

	svcs := c.clientset.CoreV1().Services(c.namespace)
	if _, err := svcs.Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{labelDeploymentID: spec.ID},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       spec.port(),
				TargetPort: intstr.FromInt(int(spec.port())),
			}},
		},
	}, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("kube: create service: %w", err)
	}

	// Stable internal Service so other flows can reach this deployment by a constant
	// name (octo-int-{slug}), load-balanced across its replicas. The slug is unique
	// per deployment, so each deployment has its own internal address.
	if err := c.ensureInternalService(ctx, spec); err != nil {
		return fmt.Errorf("kube: ensure internal service: %w", err)
	}

	// Optional external endpoint: a per-deployment Traefik Ingress with
	// cert-manager TLS at {subdomain}.{baseDomain}.
	if spec.Expose && c.baseDomain != "" {
		if _, err := c.clientset.NetworkingV1().Ingresses(c.namespace).Create(
			ctx, c.ingress(name, labels, spec), metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("kube: create ingress: %w", err)
		}
	}
	return nil
}

// ingress builds the per-deployment Traefik Ingress: host {subdomain}.{baseDomain}
// routed to the deployment's Service, with cert-manager issuing the TLS cert.
func (c *Client) ingress(name string, labels map[string]string, spec Spec) *networkingv1.Ingress {
	host := c.ExternalHost(spec.Subdomain)
	ingressClass := "traefik"
	pathType := networkingv1.PathTypePrefix
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: map[string]string{"cert-manager.io/cluster-issuer": c.clusterIssuer},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClass,
			TLS: []networkingv1.IngressTLS{{
				Hosts:      []string{host},
				SecretName: name + "-tls",
			}},
			Rules: []networkingv1.IngressRule{{
				Host: host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: name,
									Port: networkingv1.ServiceBackendPort{Number: spec.port()},
								},
							},
						}},
					},
				},
			}},
		},
	}
}

// ensureInternalService creates the stable "octo-int-{slug}" ClusterIP Service
// that selects this deployment's pods (by deployment-id label). The slug is unique
// per deployment, so the Service name is too. It is a no-op when the deployment has
// no slug or the Service already exists.
func (c *Client) ensureInternalService(ctx context.Context, spec Spec) error {
	if spec.Slug == "" {
		return nil
	}
	_, err := c.clientset.CoreV1().Services(c.namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   internalServiceName(spec.Slug),
			Labels: c.labels(spec),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{labelDeploymentID: spec.ID},
			Ports: []corev1.ServicePort{{
				Name:       "http",
				Port:       spec.port(),
				TargetPort: intstr.FromInt(int(spec.port())),
			}},
		},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// Scale updates the desired replica count of a deployment's workload via a merge
// patch on the Deployment. replicas <1 is treated as 1. A missing Deployment
// surfaces a NotFound error for the caller to handle.
func (c *Client) Scale(ctx context.Context, deploymentID string, replicas int32) error {
	if replicas < 1 {
		replicas = 1
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	_, err := c.clientset.AppsV1().Deployments(c.namespace).Patch(
		ctx, resourceName(deploymentID), types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("kube: scale deployment: %w", err)
	}
	return nil
}

// DeleteInternalService removes the stable internal Service for slug. Callers
// delete it only once the last deployment of the integration is gone; a missing
// Service is ignored.
func (c *Client) DeleteInternalService(ctx context.Context, slug string) error {
	if slug == "" {
		return nil
	}
	err := c.clientset.CoreV1().Services(c.namespace).Delete(ctx, internalServiceName(slug), metav1.DeleteOptions{})
	return ignoreNotFound(err)
}

// deployment builds the Deployment object: spec.Replicas runtime pods (clamped to
// a minimum of 1) with the integration ConfigMap mounted read-only at the config
// path, any supplied env vars set, and the runtime port declared only when the
// integration has an HTTP source (a non-networked workload exposes no port).
func (c *Client) deployment(name string, labels map[string]string, spec Spec) *appsv1.Deployment {
	replicas := spec.Replicas
	if replicas < 1 {
		replicas = 1
	}
	var ports []corev1.ContainerPort
	if spec.networked() {
		ports = []corev1.ContainerPort{{Name: "http", ContainerPort: spec.port()}}
	}
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
						Env:             envVars(spec.Env),
						Ports:           ports,
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

// envVars converts the supplied env map into a deterministically-ordered slice of
// container env vars (sorted by name so repeated Applies produce identical specs).
func envVars(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]corev1.EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, corev1.EnvVar{Name: k, Value: env[k]})
	}
	return out
}

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

// Delete removes the Deployment, Service and ConfigMap for a deployment. Missing
// resources are ignored so undeploy is safe to retry.
func (c *Client) Delete(ctx context.Context, deploymentID string) error {
	name := resourceName(deploymentID)
	del := metav1.DeleteOptions{}
	var errs []error
	// Ingress is only present for externally-exposed deployments; NotFound is
	// ignored, so deleting it unconditionally is safe.
	if err := c.clientset.NetworkingV1().Ingresses(c.namespace).Delete(ctx, name, del); ignoreNotFound(err) != nil {
		errs = append(errs, fmt.Errorf("delete ingress: %w", err))
	}
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
