// Package kube wraps the Kubernetes API access the orchestrator needs to run an
// integration as its own workload. Each deployment maps to three resources in
// the target namespace — a ConfigMap carrying the integration YAML, a Deployment
// running the generic octo-runtime image, and a ClusterIP Service — all named
// deterministically from the deployment id and labelled so they can be resolved
// without persisting their names.
//
// The package is split by concern: this file holds the Client and its
// configuration/naming helpers; deploy.go drives the per-deployment workload
// lifecycle; secret.go manages the shared cluster-secrets Secret; status.go
// computes live status and runs the informers.
package kube

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
)

const (
	labelManagedBy     = "app.kubernetes.io/managed-by"
	labelDeploymentID  = "octo.dev/deployment-id"
	labelIntegrationID = "octo.dev/integration-id"
	managedByValue     = "orchestrator"
)

// RuntimeServices configures the runtime-services environment the orchestrator
// injects into each deployed runtime pod so the runtime's k8s services module can
// reach Lease-based leader election and the orchestrator KV API. Module empty
// disables injection entirely (the runtime then falls back to its standalone
// default), which keeps the feature inert until the deploy is wired for it.
type RuntimeServices struct {
	Module          string // RUNTIME_SERVICES_MODULE for runtime pods ("" = no injection)
	OrchestratorURL string // in-cluster URL of the orchestrator KV API
	ServiceAccount  string // pod serviceAccountName granting leases RBAC ("" = default SA)
}

// Client wraps a Kubernetes clientset scoped to one namespace and runtime image.
type Client struct {
	clientset     kubernetes.Interface
	namespace     string
	runtimeImage  string
	baseDomain    string // parent domain for external endpoints ("" = disabled)
	clusterIssuer string // cert-manager ClusterIssuer for per-host external TLS
	// wildcardTLSSecret, when set, is a pre-issued *.{baseDomain} TLS Secret that
	// every per-integration ingress references instead of issuing a per-host cert
	// via clusterIssuer. Empty = per-host issuance (the clusterIssuer path).
	wildcardTLSSecret string
	// runtimeServices is the env the orchestrator injects so deployed runtime pods
	// can reach leader election + the KV API. Zero value disables injection.
	runtimeServices RuntimeServices

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
// which disables external endpoints (Apply then ignores Spec.Expose). rs carries
// the runtime-services env injected into deployed pods; its zero value disables it.
func New(namespace, runtimeImage, baseDomain, clusterIssuer, wildcardTLSSecret string, rs RuntimeServices) (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kube: in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube: clientset: %w", err)
	}
	return &Client{
		clientset:         cs,
		namespace:         namespace,
		runtimeImage:      runtimeImage,
		baseDomain:        baseDomain,
		clusterIssuer:     clusterIssuer,
		wildcardTLSSecret: wildcardTLSSecret,
		runtimeServices:   rs,
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

// ignoreNotFound returns nil for a NotFound error, passing anything else through.
func ignoreNotFound(err error) error {
	if err == nil || apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
