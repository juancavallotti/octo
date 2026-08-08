package kube

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// A dev run is the editor's "Run" workload: one pod per (user, integration), with
// an octo runtime in hot-reload mode beside a sidecar that owns its workspace.
//
// It is deliberately NOT a deployment. A deployment ships a frozen snapshot from a
// ConfigMap and is expected to outlive the session that created it; a dev run
// follows one person's editing session, reloads from the live definition, and is
// reclaimed when they stop touching it. The two share this package's plumbing —
// labels, probes, the endpoint publisher, the runtime-services env — and nothing
// else.
//
// The other deliberate difference is that **the cluster is the only record**. There
// is no dev_runs table: the workload's labels carry who owns it, one annotation
// carries when it was last used, and its existence is its status. Every identifier
// is derived from (user, integration) by the service layer, so nothing has to be
// stored to be found again.
const (
	// labelDevRunID marks a dev-run workload and carries its derived uuid. Its
	// presence is also what distinguishes a dev run from a deployment, since both
	// carry managed-by=orchestrator and an integration id.
	labelDevRunID = "octo.dev/dev-run-id"
	// labelUserID carries the owning user, so "what am I running?" is a label query.
	labelUserID = "octo.dev/user-id"

	// annLastActivity is when this dev run was last reloaded, RFC3339. The idle
	// reaper reads it; nothing else does. An annotation rather than a label because
	// a timestamp is not a selectable value and would be an invalid label value
	// anyway (colons).
	annLastActivity = "octo.dev/last-activity"
	// annTokenHash is the SHA-256 of the dev-run token the sidecar authenticates its
	// bundle pull with. Storing the hash rather than the token follows api_keys:
	// the plaintext exists only in the pod's env, so reading this grants nothing.
	// Living on the workload is what makes authorisation precise — when the workload
	// is gone, so is the credential.
	annTokenHash = "octo.dev/token-hash"
	// annHost is the external host this dev run publishes, so a listing can report
	// it without re-deriving it.
	annHost = "octo.dev/host"

	// devWorkspaceVolume is the emptyDir the two containers share. emptyDir and not
	// a PVC: the sidecar re-pulls everything on start, so nothing of value is
	// pod-scoped and a volume that outlived the pod would only be a stale copy.
	devWorkspaceVolume = "workspace"
	// devWorkspaceRoot is where the sidecar mounts the whole volume.
	devWorkspaceRoot = "/workspace"
	// devWorkspaceSubPath is the subdirectory inside the volume the runtime watches.
	devWorkspaceSubPath = "integrations"
	// devWorkspaceDir is the path both containers agree on: the sidecar writes it,
	// the runtime watches it.
	devWorkspaceDir = devWorkspaceRoot + "/" + devWorkspaceSubPath

	// devRuntimePort is the port every dev run's runtime listens on. A platform
	// constant, not a negotiated value: a dev pod owns its network namespace, so
	// there is nothing to avoid colliding with. It is injected as HTTP_PORT, which
	// the runtime resolves ahead of the definition's declared default (proved by
	// TestParseConfigSubstitutesFromOSEnv, runtime/core/runtime/env_test.go:363) —
	// so the Service can target one number forever and no port has to be recorded
	// or reconciled.
	devRuntimePort = 8080

	// defaultSidecarPort is where the sidecar serves its command API when the
	// configuration does not say. Matches the sidecar's own default
	// (sidecars/dev/main.go), so the two halves agree with nothing configured.
	defaultSidecarPort = 8099
	// sidecarPortName names the sidecar's container port.
	sidecarPortName = "sidecar"
	// sidecarReadyPath reports whether the sidecar has populated the workspace.
	sidecarReadyPath = "/readyz"

	// Env the runtime container needs. HTTP_HOST binds all interfaces so the pod is
	// reachable through its Service; HTTP_PORT pins the listen port.
	envHTTPPort = "HTTP_PORT"
	envHTTPHost = "HTTP_HOST"
	// bindAllHost is supplied as HTTP_HOST so the runtime binds all interfaces,
	// which is required for the pod to be reachable through its Service.
	bindAllHost = "0.0.0.0"

	// Env the sidecar container reads; see sidecars/dev/main.go.
	envWorkspaceDir = "WORKSPACE_DIR"
	envRuntimeAdmin = "RUNTIME_ADMIN_ADDR"
	envDevRunID     = "DEV_RUN_ID"
	envDevRunToken  = "DEV_RUN_TOKEN"
	envSidecarToken = "SIDECAR_TOKEN"

	// sidecarStartupFailureThresh bounds how long the runtime container waits for the
	// sidecar's first pull before the kubelet gives up on it: threshold × period. Set
	// generously (about two minutes) because the pull crosses to the orchestrator,
	// which may itself be rolling when a dev pod starts.
	sidecarStartupPeriodSeconds = 2
	sidecarStartupFailureThresh = 60
)

// ErrDevRunExists reports that a dev run's workload is already present, so the
// caller attached to it rather than creating one. Not a failure: two editor tabs on
// the same integration are meant to share one pod, and because the workload name is
// derived, the API server arbitrating a concurrent create is the same answer.
var ErrDevRunExists = errors.New("kube: dev run already exists")

// DevRunSpec describes the pod to create for one dev run. Every identifier in it is
// derived by the caller from (user, integration); none of it is stored anywhere.
type DevRunSpec struct {
	ID            string    // derived dev-run uuid; drives resource names and labels
	UserID        string    // owning user (label)
	IntegrationID string    // integration being edited (label)
	DevRunToken   string    // bearer the sidecar pulls its bundle with (pod env only)
	TokenHash     string    // SHA-256 of DevRunToken, recorded as an annotation
	SidecarToken  string    // bearer the orchestrator commands the sidecar with
	Networked     bool      // the definition declares an HTTP_PORT
	Subdomain     string    // derived host label; the endpoint host is {Subdomain}.{baseDomain}
	LastActivity  time.Time // seeds the idle-reaper annotation
}

// ApplyDevRun creates the dev-run workload, and its Service and external endpoint
// when the integration is networked.
//
// Unlike Apply, this is idempotent, and it has to be: the workload name is derived
// from (user, integration), so a second editor tab clicking Run computes the same
// name and should attach rather than fail. An existing workload yields
// {@link ErrDevRunExists}, which the caller reports as "attached".
func (c *Client) ApplyDevRun(ctx context.Context, spec DevRunSpec) error {
	name := devRunName(spec.ID)
	lbls := c.devRunLabels(spec)

	deps := c.clientset.AppsV1().Deployments(c.namespace)
	if _, err := deps.Create(ctx, c.devRunDeployment(name, lbls, spec), metav1.CreateOptions{}); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return ErrDevRunExists
		}
		return fmt.Errorf("kube: create dev run deployment: %w", err)
	}

	// The Service is created for EVERY dev run, networked or not, because it is how
	// the orchestrator reaches the sidecar to command it. A cron-driven integration
	// serves no HTTP and still has to be reloadable.
	if err := c.ensureDevRunService(ctx, spec, lbls); err != nil {
		return err
	}
	if !spec.Networked {
		return nil
	}
	return c.PublishDevRunEndpoint(ctx, spec)
}

// ensureDevRunService creates the ClusterIP Service for a dev run. It carries the
// sidecar port always and the runtime's HTTP port only when the integration serves
// HTTP — one Service with two purposes, because the alternative is two objects whose
// lifecycles are identical.
//
// Already-exists is fine: both ports are constants, so an existing Service is
// already correct.
func (c *Client) ensureDevRunService(ctx context.Context, spec DevRunSpec, lbls map[string]string) error {
	ports := []corev1.ServicePort{{
		Name:       sidecarPortName,
		Port:       c.sidecarPort,
		TargetPort: intstr.FromString(sidecarPortName),
	}}
	if spec.Networked {
		ports = append(ports, corev1.ServicePort{
			Name:       "http",
			Port:       devRuntimePort,
			TargetPort: intstr.FromInt(devRuntimePort),
		})
	}
	_, err := c.clientset.CoreV1().Services(c.namespace).Create(ctx, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: devRunName(spec.ID), Labels: lbls},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{labelDevRunID: spec.ID},
			Ports:    ports,
		},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("kube: create dev run service: %w", err)
	}
	return nil
}

// ReconcileDevRunService adds the HTTP port to an existing dev run's Service, for a
// reload that flipped the integration to networked. The Service is created before
// anyone knows whether the definition serves HTTP, so this is the one edit it needs.
func (c *Client) ReconcileDevRunService(ctx context.Context, spec DevRunSpec) error {
	if !spec.Networked {
		return nil
	}
	svcs := c.clientset.CoreV1().Services(c.namespace)
	svc, err := svcs.Get(ctx, devRunName(spec.ID), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("kube: get dev run service: %w", err)
	}
	for _, p := range svc.Spec.Ports {
		if p.Port == devRuntimePort {
			return nil
		}
	}
	svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
		Name:       "http",
		Port:       devRuntimePort,
		TargetPort: intstr.FromInt(devRuntimePort),
	})
	if _, err := svcs.Update(ctx, svc, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("kube: reconcile dev run service: %w", err)
	}
	return nil
}

// PublishDevRunEndpoint publishes (or re-publishes) the dev run's external endpoint
// at {Subdomain}.{baseDomain}. Separate from ApplyDevRun because a reload can flip a
// running integration to networked — an HTTP_PORT added to a definition that had
// none — and the endpoint then has to appear without recreating the pod.
//
// A no-op when external endpoints are disabled or the run declares no host, so a
// caller need not check either.
func (c *Client) PublishDevRunEndpoint(ctx context.Context, spec DevRunSpec) error {
	host := c.ExternalHost(spec.Subdomain)
	if host == "" {
		return nil
	}
	if err := c.endpoints.publish(ctx, endpoint{
		name:   devRunName(spec.ID),
		host:   host,
		port:   devRuntimePort,
		labels: c.devRunLabels(spec),
	}); err != nil {
		return fmt.Errorf("kube: publish dev run endpoint: %w", err)
	}
	return nil
}

// WithdrawDevRunEndpoint removes the dev run's external endpoint, for the reverse
// flip: an HTTP_PORT removed from a definition should stop the public host serving
// rather than leave it pointed at a runtime that no longer listens.
func (c *Client) WithdrawDevRunEndpoint(ctx context.Context, devRunID string) error {
	if err := c.endpoints.withdraw(ctx, devRunName(devRunID)); err != nil {
		return fmt.Errorf("kube: withdraw dev run endpoint: %w", err)
	}
	return nil
}

// DeleteDevRun removes the dev run's endpoint, Deployment and Service. Missing
// resources are ignored, so a stop is safe to retry and safe to race with a reap.
func (c *Client) DeleteDevRun(ctx context.Context, devRunID string) error {
	name := devRunName(devRunID)
	del := metav1.DeleteOptions{}
	var errs []error
	// The endpoint exists only for networked runs; the publisher ignores NotFound, so
	// withdrawing unconditionally is safe.
	if err := c.endpoints.withdraw(ctx, name); err != nil {
		errs = append(errs, err)
	}
	if err := c.clientset.AppsV1().Deployments(c.namespace).Delete(ctx, name, del); ignoreNotFound(err) != nil {
		errs = append(errs, fmt.Errorf("delete deployment: %w", err))
	}
	if err := c.clientset.CoreV1().Services(c.namespace).Delete(ctx, name, del); ignoreNotFound(err) != nil {
		errs = append(errs, fmt.Errorf("delete service: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("kube: delete dev run %s: %w", name, errors.Join(errs...))
	}
	return nil
}

// TouchDevRun records that the dev run was just used, which is what keeps the idle
// reaper from collecting it.
//
// A merge patch of the one annotation, not a read-modify-write: two orchestrator
// replicas can bump it concurrently and neither can lose the other's value or fail
// on a conflict. A missing workload is reported, because a reload that touched
// nothing is a dev run the caller thinks exists and does not.
func (c *Client) TouchDevRun(ctx context.Context, devRunID string, at time.Time) error {
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%q}}}`,
		annLastActivity, at.UTC().Format(time.RFC3339))
	_, err := c.clientset.AppsV1().Deployments(c.namespace).Patch(
		ctx, devRunName(devRunID), types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("kube: touch dev run: %w", err)
	}
	return nil
}

// SetDevRunHost records (or clears) the external host a dev run publishes, for a
// reload that flipped the integration between serving HTTP and not.
//
// A merge patch like TouchDevRun, for the same reason — two replicas must not be able
// to conflict — and an empty host patches the key to null, which is how a JSON merge
// patch removes it. Clearing rather than blanking matters: an empty-string annotation
// and an absent one would then both mean "not published", and only one of them would
// be what a reader expects.
func (c *Client) SetDevRunHost(ctx context.Context, devRunID, host string) error {
	value := "null"
	if host != "" {
		value = fmt.Sprintf("%q", host)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q:%s}}}`, annHost, value)
	_, err := c.clientset.AppsV1().Deployments(c.namespace).Patch(
		ctx, devRunName(devRunID), types.MergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("kube: set dev run host: %w", err)
	}
	return nil
}

// DevRun is one live dev-run workload, read back from the cluster. Everything here
// comes off the object itself — there is no other copy to reconcile it with.
type DevRun struct {
	ID            string
	UserID        string
	IntegrationID string
	Host          string    // external host, "" when no endpoint is published
	TokenHash     string    // SHA-256 of the dev-run token, for authorising its sidecar
	LastActivity  time.Time // zero when the annotation is absent or unparseable
	CreatedAt     time.Time
	Status        Status
}

// DevRunSelector narrows a dev-run listing. A zero selector lists every dev run in
// the namespace, which is what the idle reaper wants; the editor sets both fields.
type DevRunSelector struct {
	UserID        string
	IntegrationID string
}

// ListDevRuns returns the dev-run workloads matching sel, each with live status.
//
// This is the query that replaces a database: "is something running for this
// integration?" and "what am I running?" are both label lookups against the
// informer cache, which cannot disagree with what is actually running.
func (c *Client) ListDevRuns(ctx context.Context, sel DevRunSelector) ([]DevRun, error) {
	selector, err := devRunSelector(sel)
	if err != nil {
		return nil, err
	}
	deps, pods, err := c.listDevRunWorkloads(ctx, selector)
	if err != nil {
		return nil, err
	}

	out := make([]DevRun, 0, len(deps))
	for _, dep := range deps {
		id := dep.Labels[labelDevRunID]
		out = append(out, DevRun{
			ID:            id,
			UserID:        dep.Labels[labelUserID],
			IntegrationID: dep.Labels[labelIntegrationID],
			Host:          dep.Annotations[annHost],
			TokenHash:     dep.Annotations[annTokenHash],
			LastActivity:  parseLastActivity(dep.Annotations[annLastActivity]),
			CreatedAt:     dep.CreationTimestamp.Time,
			Status:        computeStatus(dep, podsFor(pods, id)),
		})
	}
	return out, nil
}

// listDevRunWorkloads reads the Deployments matching selector plus every dev-run
// pod, preferring the informer cache. Pods are fetched once for the whole listing
// rather than per workload: a reaper sweep over N dev runs should be two reads, not
// 2N.
func (c *Client) listDevRunWorkloads(
	ctx context.Context, selector labels.Selector,
) ([]*appsv1.Deployment, []*corev1.Pod, error) {
	if c.synced != nil && c.synced() {
		deps, err := c.depLister.List(selector)
		if err != nil {
			return nil, nil, fmt.Errorf("kube: lister list dev runs: %w", err)
		}
		pods, err := c.podLister.List(selector)
		if err != nil {
			return nil, nil, fmt.Errorf("kube: lister list dev run pods: %w", err)
		}
		return deps, pods, nil
	}

	depList, err := c.clientset.AppsV1().Deployments(c.namespace).List(ctx,
		metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, nil, fmt.Errorf("kube: list dev runs: %w", err)
	}
	podList, err := c.clientset.CoreV1().Pods(c.namespace).List(ctx,
		metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, nil, fmt.Errorf("kube: list dev run pods: %w", err)
	}
	deps := make([]*appsv1.Deployment, len(depList.Items))
	for i := range depList.Items {
		deps[i] = &depList.Items[i]
	}
	pods := make([]*corev1.Pod, len(podList.Items))
	for i := range podList.Items {
		pods[i] = &podList.Items[i]
	}
	return deps, pods, nil
}

// DevRunStatus reports the live status of one dev run. A missing workload reads as
// failed, matching Status for deployments — except that for a dev run "missing"
// usually means stopped, which is why callers check the listing rather than this to
// decide whether one exists.
func (c *Client) DevRunStatus(ctx context.Context, devRunID string) (Status, error) {
	runs, err := c.ListDevRuns(ctx, DevRunSelector{})
	if err != nil {
		return Status{}, err
	}
	for _, run := range runs {
		if run.ID == devRunID {
			return run.Status, nil
		}
	}
	return Status{Phase: StatusFailed}, nil
}

// devRunSelector builds the label selector for a dev-run listing: managed by us,
// carrying a dev-run id at all (which is what excludes deployments), and narrowed
// by whichever of user/integration the caller supplied.
func devRunSelector(sel DevRunSelector) (labels.Selector, error) {
	exists, err := labels.NewRequirement(labelDevRunID, selection.Exists, nil)
	if err != nil {
		return nil, fmt.Errorf("kube: dev run selector: %w", err)
	}
	out := labels.SelectorFromSet(labels.Set{labelManagedBy: managedByValue}).Add(*exists)
	if sel.UserID != "" {
		req, err := labels.NewRequirement(labelUserID, selection.Equals, []string{sel.UserID})
		if err != nil {
			return nil, fmt.Errorf("kube: dev run selector user: %w", err)
		}
		out = out.Add(*req)
	}
	if sel.IntegrationID != "" {
		req, err := labels.NewRequirement(labelIntegrationID, selection.Equals, []string{sel.IntegrationID})
		if err != nil {
			return nil, fmt.Errorf("kube: dev run selector integration: %w", err)
		}
		out = out.Add(*req)
	}
	return out, nil
}

// podsFor filters a pod list down to one dev run's pods.
func podsFor(pods []*corev1.Pod, devRunID string) []*corev1.Pod {
	var out []*corev1.Pod
	for _, p := range pods {
		if p.Labels[labelDevRunID] == devRunID {
			out = append(out, p)
		}
	}
	return out
}

// parseLastActivity reads the idle-reaper annotation, returning the zero time when
// it is absent or malformed. The reaper decides what a zero means; this only reports
// what the object says.
func parseLastActivity(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return at
}

// devRunDeployment builds the two-container pod: the sidecar that owns the
// workspace and the runtime that watches it, sharing an emptyDir.
//
// The sidecar is a NATIVE sidecar — an initContainers entry with
// restartPolicy: Always (GA in Kubernetes 1.29). That is not stylistic. `octo run
// --watch` tolerates a missing config but not a missing directory: its watcher's
// fsnotify Add fails and the process exits (runtime/octo/watch.go:29). Running the
// sidecar as an init container with a startupProbe on its /readyz means the runtime
// container does not start until the first pull has landed, so `octo` finds a
// populated directory on its first look instead of racing it.
//
// The subPath mount on the runtime container is the belt to that brace: kubelet
// creates the subdirectory, so the directory exists even if the sidecar somehow
// has not written it.
func (c *Client) devRunDeployment(name string, lbls map[string]string, spec DevRunSpec) *appsv1.Deployment {
	replicas := int32(1)
	always := corev1.ContainerRestartPolicyAlways
	lastActivity := spec.LastActivity
	if lastActivity.IsZero() {
		lastActivity = time.Now()
	}

	annotations := map[string]string{
		annLastActivity: lastActivity.UTC().Format(time.RFC3339),
		annTokenHash:    spec.TokenHash,
	}
	// Only a networked run records a host, because only a networked run gets an
	// endpoint published. Recording one regardless would advertise a hostname that
	// resolves and then 502s, and would make the annotation useless for the question a
	// reload has to answer: is an endpoint currently live for this run?
	if spec.Networked {
		if host := c.ExternalHost(spec.Subdomain); host != "" {
			annotations[annHost] = host
		}
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: lbls, Annotations: annotations},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{labelDevRunID: spec.ID},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: lbls},
				Spec: corev1.PodSpec{
					// No ServiceAccountName, deliberately. A deployment's runtime pod needs
					// the runtime ServiceAccount for Lease-based leader election; the
					// standalone build does no leader election and talks to no cluster API,
					// so a dev pod runs on the namespace default with no credential at all.
					ImagePullSecrets: c.pullSecretRefs(),
					InitContainers: []corev1.Container{{
						Name:            "sidecar",
						Image:           c.sidecarImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						RestartPolicy:   &always,
						Env:             c.sidecarEnv(spec),
						Ports: []corev1.ContainerPort{{
							Name:          sidecarPortName,
							ContainerPort: c.sidecarPort,
						}},
						// Gates the runtime container's start on the first pull having landed.
						StartupProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: sidecarReadyPath,
									Port: intstr.FromString(sidecarPortName),
								},
							},
							PeriodSeconds:    sidecarStartupPeriodSeconds,
							TimeoutSeconds:   probeTimeoutSeconds,
							FailureThreshold: sidecarStartupFailureThresh,
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      devWorkspaceVolume,
							MountPath: devWorkspaceRoot,
						}},
					}},
					Containers: []corev1.Container{{
						Name: RuntimeContainer,
						// The STANDALONE runtime image, not the one the orchestrator deploys.
						// The two builds are disjoint — providers_k8s.go and
						// providers_standalone.go are mutually exclusive build tags — and a dev
						// run wants the standalone one: no log shipping to the aggregator, no
						// cluster queues, no leader election, and a resource loader rooted at
						// the config directory the sidecar writes.
						Image:           c.devRuntimeImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						// The image's default command points at /etc/octo/integrations and does
						// not watch; a dev run watches the shared workspace instead.
						Args:           []string{"run", "--config", devWorkspaceDir, "--watch"},
						Env:            devRuntimeEnv(spec),
						Ports:          devRuntimePorts(spec),
						LivenessProbe:  livenessProbe(),
						ReadinessProbe: readinessProbe(),
						VolumeMounts: []corev1.VolumeMount{{
							Name:      devWorkspaceVolume,
							MountPath: devWorkspaceDir,
							SubPath:   devWorkspaceSubPath,
							// Writable, unlike a deployment's read-only ConfigMap mount: this is
							// the same volume the sidecar writes, and a read-only mount of it
							// would be a lie about who owns the directory.
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: devWorkspaceVolume,
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}},
				},
			},
		},
	}
}

// devRunPorts declares the runtime container's ports: the admin port always, and
// the HTTP port only when the integration serves HTTP.
func devRuntimePorts(spec DevRunSpec) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{{Name: adminPortName, ContainerPort: adminPort}}
	if spec.Networked {
		ports = append(ports, corev1.ContainerPort{Name: "http", ContainerPort: devRuntimePort})
	}
	return ports
}

// devRuntimeEnv is the runtime container's env, and it is short because a dev run
// uses the STANDALONE runtime image.
//
// No RUNTIME_SERVICES_MODULE, no OCTO_DEPLOYMENT_ID, no NATS_URL, no
// ServiceAccount: the standalone build ships only the standalone services provider
// (runtime/octo/providers_standalone.go), so a dev run needs none of the cluster
// wiring a deployment does. Everything that follows from that is a simplification —
// it writes no kv_store rows, so there is nothing to clean up on stop; it ships no
// logs to the aggregator, so its output is plain pod stdout, which is what
// PodLogs streams anyway; and it needs no RBAC at all, so a dev pod runs with no
// cluster credential of any kind.
//
// It also makes the pull model coherent rather than merely workable: the standalone
// provider's resource loader is rooted at the config directory, which is exactly the
// directory the sidecar stages resources into. The k8s provider would instead fetch
// resources from the orchestrator by snapshot id — a snapshot a dev run does not
// have.
func devRuntimeEnv(spec DevRunSpec) []corev1.EnvVar {
	if !spec.Networked {
		return nil
	}
	return []corev1.EnvVar{
		{Name: envHTTPPort, Value: fmt.Sprintf("%d", devRuntimePort)},
		{Name: envHTTPHost, Value: bindAllHost},
	}
}

// sidecarEnv is the sidecar container's env; see sidecars/dev/main.go for what each
// value means. The two tokens are distinct on purpose: DEV_RUN_TOKEN authorises the
// pod's outbound pull and its hash is recorded on the workload, while SIDECAR_TOKEN
// authorises the orchestrator's inbound commands. One token doing both jobs would
// mean a leak in either direction compromised both.
func (c *Client) sidecarEnv(spec DevRunSpec) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "PORT", Value: fmt.Sprintf("%d", c.sidecarPort)},
		{Name: envWorkspaceDir, Value: devWorkspaceDir},
		{Name: envRuntimeAdmin, Value: fmt.Sprintf("127.0.0.1:%d", adminPort)},
		{Name: envOrchestrator, Value: c.orchestratorURL},
		{Name: envDevRunID, Value: spec.ID},
		{Name: envDevRunToken, Value: spec.DevRunToken},
		{Name: envSidecarToken, Value: spec.SidecarToken},
	}
}

// devRunLabels are the labels every object of one dev run carries. managed-by
// matches deployments so the existing informers see dev runs too; the dev-run id is
// what tells the two apart.
func (c *Client) devRunLabels(spec DevRunSpec) map[string]string {
	return map[string]string{
		labelManagedBy:     managedByValue,
		labelDevRunID:      spec.ID,
		labelUserID:        spec.UserID,
		labelIntegrationID: spec.IntegrationID,
	}
}

// SidecarURL is the in-cluster address of a dev run's sidecar API. The Service
// fronts the runtime's HTTP port, not the sidecar's, so commands address the pod
// through the headless-style per-dev-run Service name on the sidecar port.
func (c *Client) SidecarURL(devRunID string) string {
	return fmt.Sprintf("http://%s.%s:%d", devRunName(devRunID), c.namespace, c.sidecarPort)
}

// devRunName is the deterministic name shared by a dev run's objects. "octo-dev-"
// plus a uuid stays within the 63-char DNS-1123 label limit, and mirrors
// resourceName's "octo-dep-" so the two kinds of workload are distinguishable at a
// glance in kubectl output.
func devRunName(devRunID string) string { return "octo-dev-" + devRunID }
