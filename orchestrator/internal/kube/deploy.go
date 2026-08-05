package kube

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

	// adminPort is where the runtime's observability service serves its probes. It
	// matches that service's default (--observability-addr :39999) and is present on
	// every deployment, networked or not: a cron-driven integration needs probes
	// exactly as much as an HTTP one.
	adminPort = 39999
	// adminPortName names the container port the probes target.
	adminPortName = "admin"
	// livenessPath answers whether the process is wedged; readinessPath answers
	// whether it is serving. See the runtime's observability service.
	livenessPath  = "/healthz"
	readinessPath = "/readyz"

	// Probe timings. Readiness is checked often and reacts fast, because it gates
	// traffic: a rolling update should not send requests to a pod that is still
	// starting connectors, and a pod that goes unready should leave the Service
	// promptly. Liveness is checked rarely and forgivingly, because it restarts the
	// container: a slow start or a brief stall must not be mistaken for a wedge.
	readinessPeriodSeconds   = 5
	readinessFailureThresh   = 3
	livenessPeriodSeconds    = 30
	livenessFailureThresh    = 6
	livenessInitialDelaySecs = 15
	probeTimeoutSeconds      = 3

	// Runtime-services env var names injected into each deployed pod. They mirror
	// the constants the runtime's k8s services module reads (RUNTIME_SERVICES_MODULE
	// selects the backend; the rest identify the deployment and the KV endpoint, with
	// POD_NAME/POD_NAMESPACE sourced from the downward API).
	envServicesModule = "RUNTIME_SERVICES_MODULE"
	envDeploymentID   = "OCTO_DEPLOYMENT_ID"
	envDeploymentName = "OCTO_DEPLOYMENT_NAME"
	envDeploymentVer  = "OCTO_DEPLOYMENT_VERSION"
	envSnapshotID     = "OCTO_SNAPSHOT_ID"
	envOrchestrator   = "ORCHESTRATOR_URL"
	envNATSURL        = "NATS_URL"
	envPodName        = "POD_NAME"
	envPodNamespace   = "POD_NAMESPACE"
)

// Spec describes the workload to create for one deployment.
type Spec struct {
	ID            string            // deployment uuid; drives resource names and labels
	IntegrationID string            // owning integration uuid (label + internal Service selector)
	Name          string            // deployment display name, stamped onto shipped logs
	Version       string            // deployment tag/version, stamped onto shipped logs ("" = untagged)
	SnapshotID    string            // snapshot the definition/resources were frozen under ("" = untagged); the runtime loads its frozen resources by it
	Definition    string            // runtime-loadable integration YAML
	Replicas      int32             // desired replica count; <1 is treated as 1
	Slug          string            // unique slug naming this deployment's internal Service ("" = none)
	Port          int               // runtime HTTP port (from HTTP_PORT); 0 means no HTTP source (no Service)
	Env           map[string]string // literal env vars supplied to the runtime container (e.g. HTTP_HOST/HTTP_PORT)
	SecretEnv     map[string]string // env-var name → cluster-secret key, injected via secretKeyRef (disjoint from Env)
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

	// Optional external endpoint at {subdomain}.{baseDomain}, published as
	// whichever object this cluster routes with — see endpoint.go.
	if spec.Expose && c.baseDomain != "" {
		if err := c.endpoints.publish(ctx, endpoint{
			name:   name,
			host:   c.ExternalHost(spec.Subdomain),
			port:   spec.port(),
			labels: labels,
		}); err != nil {
			return fmt.Errorf("kube: publish endpoint: %w", err)
		}
	}
	return nil
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

// configHashAnnotation carries a digest of the mounted definition on the pod
// template. Bumping it on a rollout changes the pod spec, so Kubernetes performs a
// rolling update even when only the ConfigMap content changed (a ConfigMap data
// change alone does not restart pods).
const configHashAnnotation = "octo.dev/config-hash"

// Rollout updates a live deployment to a new definition in place: it rewrites the
// ConfigMap and updates the Deployment to the rebuilt spec, stamping a fresh config
// hash on the pod template so Kubernetes rolls the pods. Resource names, the
// Selector and the Services are unchanged, so the deployment keeps its address and
// identity. A missing Deployment/ConfigMap surfaces an error for the caller.
func (c *Client) Rollout(ctx context.Context, spec Spec) error {
	name := resourceName(spec.ID)
	labels := c.labels(spec)

	cms := c.clientset.CoreV1().ConfigMaps(c.namespace)
	cm, err := cms.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("kube: rollout get configmap: %w", err)
	}
	cm.Data = map[string]string{configFileName: spec.Definition}
	if _, err := cms.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("kube: rollout update configmap: %w", err)
	}

	deps := c.clientset.AppsV1().Deployments(c.namespace)
	existing, err := deps.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("kube: rollout get deployment: %w", err)
	}
	desired := c.deployment(name, labels, spec)
	// Carry the resourceVersion so the update is a clean replace of the existing
	// object (the Selector is unchanged, so the immutable field is preserved).
	desired.ResourceVersion = existing.ResourceVersion
	if desired.Spec.Template.ObjectMeta.Annotations == nil {
		desired.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	desired.Spec.Template.ObjectMeta.Annotations[configHashAnnotation] = configHash(spec.Definition)
	if _, err := deps.Update(ctx, desired, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("kube: rollout update deployment: %w", err)
	}
	return nil
}

// configHash returns a short digest of a definition, used to force a pod-template
// change (and thus a rolling update) when the definition changes.
func configHash(definition string) string {
	sum := sha256.Sum256([]byte(definition))
	return hex.EncodeToString(sum[:8])
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
// path, any supplied env vars set, the runtime port declared only when the
// integration has an HTTP source (a non-networked workload exposes no port), and
// probes on the admin port every deployment has.
func (c *Client) deployment(name string, labels map[string]string, spec Spec) *appsv1.Deployment {
	replicas := spec.Replicas
	if replicas < 1 {
		replicas = 1
	}
	// The admin port is always declared: the observability service is compiled into
	// the runtime image and on by default, so every pod serves probes whether or
	// not the integration serves HTTP.
	ports := []corev1.ContainerPort{{Name: adminPortName, ContainerPort: adminPort}}
	if spec.networked() {
		ports = append(ports, corev1.ContainerPort{Name: "http", ContainerPort: spec.port()})
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
					// Empty when runtime services are not wired; an empty name leaves the
					// pod on the namespace's default ServiceAccount.
					ServiceAccountName: c.runtimeServices.ServiceAccount,
					// Nil unless the runtime image needs credentials. Without this an
					// install that mirrors the images into a private registry comes up
					// perfectly — the chart puts pull secrets on its own workloads — and
					// then every integration deployed from that healthy editor sits in
					// ErrImagePull, which reads as a broken deploy rather than a missing
					// credential.
					ImagePullSecrets: c.pullSecretRefs(),
					Containers: []corev1.Container{{
						Name:            "runtime",
						Image:           c.runtimeImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Env:             c.podEnv(spec),
						Ports:           ports,
						LivenessProbe:   livenessProbe(),
						ReadinessProbe:  readinessProbe(),
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

// pullSecretRefs converts the configured Secret names into the reference list a
// PodSpec takes. Nil for none, rather than an empty slice: an empty
// imagePullSecrets field is a no-op to the API server but a diff to anything
// comparing specs, and this one is compared on every reconcile.
func (c *Client) pullSecretRefs() []corev1.LocalObjectReference {
	if len(c.imagePullSecrets) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(c.imagePullSecrets))
	for _, name := range c.imagePullSecrets {
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return refs
}

// readinessProbe gates traffic on the runtime actually serving: /readyz answers
// 200 only once every connector and flow of the current generation has started,
// and 503 while it is starting, reloading under --watch, or draining. It is
// checked often and gives up quickly, so a rolling update does not send requests
// to a pod that is not ready and an unready pod leaves the Service promptly.
//
// It has no initial delay: answering during startup is the whole point, and the
// admin server binds before connectors do.
func readinessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:     httpProbe(readinessPath),
		PeriodSeconds:    readinessPeriodSeconds,
		TimeoutSeconds:   probeTimeoutSeconds,
		FailureThreshold: readinessFailureThresh,
	}
}

// livenessProbe restarts a wedged container. It is deliberately slower and more
// forgiving than readiness, because the cost of a false positive is a restart:
// /healthz answers unconditionally, so a failure means the process could not
// serve a trivial request at all.
func livenessProbe() *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:        httpProbe(livenessPath),
		InitialDelaySeconds: livenessInitialDelaySecs,
		PeriodSeconds:       livenessPeriodSeconds,
		TimeoutSeconds:      probeTimeoutSeconds,
		FailureThreshold:    livenessFailureThresh,
	}
}

// httpProbe builds an HTTP GET handler against the admin port, addressed by name
// so the port number lives in exactly one place.
func httpProbe(path string) corev1.ProbeHandler {
	return corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Path: path,
			Port: intstr.FromString(adminPortName),
		},
	}
}

// podEnv is the full runtime container env: the orchestrator-injected runtime-
// services vars (when wired) followed by the user's literal/secret bindings. The
// two groups are each deterministic, so repeated Applies produce identical specs.
// When neither group has entries the result is nil, matching a bare workload.
func (c *Client) podEnv(spec Spec) []corev1.EnvVar {
	rs := c.runtimeServicesEnv(spec)
	user := containerEnv(spec)
	if len(rs) == 0 {
		return user
	}
	return append(rs, user...)
}

// runtimeServicesEnv builds the env the runtime's k8s services module reads:
// the selected backend, the deployment id and orchestrator KV URL, the NATS broker
// URL backing the queues, plus POD_NAME/POD_NAMESPACE from the downward API. It is
// empty unless a module is configured, so deployments stay unchanged until the
// runtime-services env is wired in. NATS_URL is emitted only when set, so a deploy
// without a broker injects nothing for it.
func (c *Client) runtimeServicesEnv(spec Spec) []corev1.EnvVar {
	if c.runtimeServices.Module == "" {
		return nil
	}
	env := []corev1.EnvVar{
		{Name: envServicesModule, Value: c.runtimeServices.Module},
		{Name: envDeploymentID, Value: spec.ID},
		{Name: envOrchestrator, Value: c.runtimeServices.OrchestratorURL},
		{Name: envPodName, ValueFrom: fieldRef("metadata.name")},
		{Name: envPodNamespace, ValueFrom: fieldRef("metadata.namespace")},
	}
	if c.runtimeServices.NATSURL != "" {
		env = append(env, corev1.EnvVar{Name: envNATSURL, Value: c.runtimeServices.NATSURL})
	}
	// Deployment identity stamped onto shipped logs. Emitted only when set, so an
	// untagged or unnamed deployment injects nothing rather than an empty var.
	if spec.Name != "" {
		env = append(env, corev1.EnvVar{Name: envDeploymentName, Value: spec.Name})
	}
	if spec.Version != "" {
		env = append(env, corev1.EnvVar{Name: envDeploymentVer, Value: spec.Version})
	}
	// The snapshot id lets the runtime's resource loader fetch this deployment's
	// frozen resources from the orchestrator. Emitted only for tagged deploys;
	// without it the runtime falls back to loading no resources.
	if spec.SnapshotID != "" {
		env = append(env, corev1.EnvVar{Name: envSnapshotID, Value: spec.SnapshotID})
	}
	return env
}

// fieldRef is a downward-API env source reading a pod field (e.g. metadata.name).
func fieldRef(path string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: path}}
}

// containerEnv builds the runtime container's env from the literal values in
// spec.Env and the cluster-secret references in spec.SecretEnv, as a single slice
// sorted by name so repeated Applies produce identical specs. A name present in
// both maps takes its literal value: the service keeps the two disjoint, so this
// is only a defensive tie-break. Secret references use Optional=false, so a pod
// referencing a missing key fails to start (surfaced as a terminal status)
// rather than silently running without the value.
func containerEnv(spec Spec) []corev1.EnvVar {
	if len(spec.Env) == 0 && len(spec.SecretEnv) == 0 {
		return nil
	}
	names := make([]string, 0, len(spec.Env)+len(spec.SecretEnv))
	for k := range spec.Env {
		names = append(names, k)
	}
	for k := range spec.SecretEnv {
		if _, dup := spec.Env[k]; !dup {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	optional := false
	out := make([]corev1.EnvVar, 0, len(names))
	for _, k := range names {
		if v, ok := spec.Env[k]; ok {
			out = append(out, corev1.EnvVar{Name: k, Value: v})
			continue
		}
		out = append(out, corev1.EnvVar{
			Name: k,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretsName},
					Key:                  spec.SecretEnv[k],
					Optional:             &optional,
				},
			},
		})
	}
	return out
}

// Delete removes the Deployment, Service and ConfigMap for a deployment. Missing
// resources are ignored so undeploy is safe to retry.
func (c *Client) Delete(ctx context.Context, deploymentID string) error {
	name := resourceName(deploymentID)
	del := metav1.DeleteOptions{}
	var errs []error
	// The endpoint object exists only for externally-exposed deployments; the
	// publisher ignores NotFound, so withdrawing unconditionally is safe.
	if err := c.endpoints.withdraw(ctx, name); err != nil {
		errs = append(errs, err)
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
