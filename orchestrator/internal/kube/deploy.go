package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
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
)

// Spec describes the workload to create for one deployment.
type Spec struct {
	ID            string            // deployment uuid; drives resource names and labels
	IntegrationID string            // owning integration uuid (label + internal Service selector)
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
						Env:             containerEnv(spec),
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
