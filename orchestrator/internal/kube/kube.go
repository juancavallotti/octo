// Package kube wraps the Kubernetes API access the orchestrator needs to run an
// integration as its own workload. Each deployment maps to three resources in
// the target namespace — a ConfigMap carrying the integration YAML, a Deployment
// running the generic octo-runtime image, and a ClusterIP Service — all named
// deterministically from the deployment id and labelled so they can be resolved
// without persisting their names.
//
// The package is split by concern: this file holds the Client and its
// configuration/naming helpers; deploy.go drives the per-deployment workload
// lifecycle; endpoint.go publishes the external endpoint of an exposed
// deployment; secret.go manages the shared cluster-secrets Secret; status.go
// computes live status and runs the informers.
package kube

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
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
	NATSURL         string // in-cluster URL of the NATS broker backing the queues ("" = omit)
}

// Client wraps a Kubernetes clientset scoped to one namespace and runtime image.
type Client struct {
	clientset    kubernetes.Interface
	namespace    string
	runtimeImage string
	baseDomain   string // parent domain for external endpoints ("" = disabled)
	// endpoints publishes the external endpoint of an exposed deployment. Which
	// implementation it holds — Ingress or Gateway API HTTPRoute — is the only
	// place the two differ; see endpoint.go.
	endpoints endpointPublisher
	// imagePullSecrets names Secrets in this namespace that authenticate the pull
	// of runtimeImage. Nil is the normal case — the image is public, or the nodes
	// carry credentials — and anything else mirrors what the chart puts on its own
	// workloads, since a mirrored registry holds every octo image, not four of five.
	imagePullSecrets []string
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

// EndpointAPI names the Kubernetes API used to publish per-integration external
// endpoints. These are the only two APIs Kubernetes has for the job.
type EndpointAPI string

const (
	// EndpointAPIIngress publishes a networking.k8s.io Ingress per exposed
	// deployment. The default: built into every cluster.
	EndpointAPIIngress EndpointAPI = "ingress"
	// EndpointAPIGateway publishes a gateway.networking.k8s.io HTTPRoute per
	// exposed deployment, attached to an existing Gateway. Requires the Gateway
	// API CRDs, which are not built in — see routePublisher.preflight.
	EndpointAPIGateway EndpointAPI = "gateway"
)

// ParseEndpointAPI converts a configured value into an EndpointAPI. Empty means
// the default. An unrecognised value is an error rather than a fallback: falling
// back to Ingress on a Gateway API cluster produces objects no controller
// serves, and nothing anywhere calls that a failure.
func ParseEndpointAPI(s string) (EndpointAPI, error) {
	switch EndpointAPI(s) {
	case "":
		return EndpointAPIIngress, nil
	case EndpointAPIIngress:
		return EndpointAPIIngress, nil
	case EndpointAPIGateway:
		return EndpointAPIGateway, nil
	default:
		return "", fmt.Errorf("kube: unknown endpoint API %q (want %q or %q)", s, EndpointAPIIngress, EndpointAPIGateway)
	}
}

// GatewayRef identifies the Gateway that per-integration HTTPRoutes attach to.
// The Gateway is not created or owned by octo: it is the cluster's ingress
// infrastructure, with one *.{baseDomain} listener, and octo attaches routes to
// it. That split — the cluster owner owns the Gateway, octo owns the routes in
// its own namespace — is most of why this mode is worth having.
type GatewayRef struct {
	// Name of the Gateway. Required when EndpointAPI is gateway and external
	// endpoints are enabled; there is no sensible default to guess.
	Name string
	// Namespace holding it. Empty means the orchestrator's own namespace. A
	// Gateway elsewhere must permit attachment from ours through its listener's
	// allowedRoutes — nothing octo can set from this side.
	Namespace string
	// SectionName names one listener on the Gateway. Empty attaches to every
	// listener that accepts the hostname, which on a Gateway serving both HTTP
	// and HTTPS also publishes the endpoint in plaintext.
	SectionName string
}

// Config is everything the Client needs to know about the cluster it deploys
// into. Every field is optional in the sense that its zero value is a coherent
// choice — no external endpoints, no TLS annotation, the cluster's default
// IngressClass — so a caller supplies only what its cluster actually has. The
// per-field meanings are the Client's, documented above.
//
// It is a struct rather than parameters because these are settings of the same
// kind (cluster facts read from the environment), several of them adjacent
// strings, and a call site passing five strings positionally is one
// transposition away from deploying with the ClusterIssuer as its ingress class.
//
// The Ingress fields (ClusterIssuer through ExtraAnnotations) and the Gateway
// field are alternatives, selected by EndpointAPI; whichever set does not apply
// is ignored rather than rejected, because a values file that carries both is a
// cluster in the middle of moving between them.
type Config struct {
	Namespace         string
	RuntimeImage      string
	BaseDomain        string
	EndpointAPI       EndpointAPI
	ClusterIssuer     string
	WildcardTLSSecret string
	IngressClass      string
	ExtraAnnotations  map[string]string
	Gateway           GatewayRef
	ImagePullSecrets  []string
	RuntimeServices   RuntimeServices
}

// Validate reports configuration that cannot work, before anything is built from
// it. It touches no cluster: this is the class of mistake that should stop the
// process at startup with the setting named, not surface as a deploy that
// creates a route pointing at nothing.
func (c Config) Validate() error {
	// newClient treats anything that is not gateway as ingress, which is the right
	// default for the zero value and the wrong one for a typo — a Config built in
	// code rather than through ParseEndpointAPI would otherwise select Ingress on
	// a cluster that has none. Rejecting here makes the field mean what its
	// documentation says regardless of who filled it in.
	switch c.EndpointAPI {
	case "", EndpointAPIIngress, EndpointAPIGateway:
	default:
		return fmt.Errorf("kube: unknown endpoint API %q (want %q or %q)",
			c.EndpointAPI, EndpointAPIIngress, EndpointAPIGateway)
	}
	if c.EndpointAPI == EndpointAPIGateway && c.BaseDomain != "" && c.Gateway.Name == "" {
		return fmt.Errorf("kube: gateway endpoints need a Gateway to attach to: set the gateway name")
	}
	return nil
}

// New builds a Client from the in-cluster config. It returns an error when the
// orchestrator is not running inside a cluster (e.g. local `go run`), letting the
// caller disable deployment features rather than crash.
func New(cfg Config) (*Client, error) {
	rc, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("kube: in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("kube: clientset: %w", err)
	}
	// Built unconditionally, and cheaply: constructing the client only prepares
	// REST plumbing, so it costs nothing on a cluster with no Gateway API. Whether
	// the CRDs are actually there is Preflight's question.
	gwcs, err := gatewayclient.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("kube: gateway clientset: %w", err)
	}
	return newClient(cfg, cs, gwcs), nil
}

// newClient assembles a Client around already-built API clients. New supplies
// real ones; the tests supply fakes. It is the single place the configuration
// becomes a Client, so the two paths cannot drift.
func newClient(cfg Config, cs kubernetes.Interface, gwcs gatewayclient.Interface) *Client {
	c := &Client{
		clientset:        cs,
		namespace:        cfg.Namespace,
		runtimeImage:     cfg.RuntimeImage,
		baseDomain:       cfg.BaseDomain,
		imagePullSecrets: cfg.ImagePullSecrets,
		runtimeServices:  cfg.RuntimeServices,
	}
	if cfg.EndpointAPI == EndpointAPIGateway {
		c.endpoints = &routePublisher{client: gwcs, namespace: cfg.Namespace, gateway: cfg.Gateway}
	} else {
		c.endpoints = &ingressPublisher{
			clientset:         cs,
			namespace:         cfg.Namespace,
			clusterIssuer:     cfg.ClusterIssuer,
			wildcardTLSSecret: cfg.WildcardTLSSecret,
			ingressClass:      cfg.IngressClass,
			extraAnnotations:  cfg.ExtraAnnotations,
		}
	}
	return c
}

// Preflight verifies the cluster can serve the endpoints this Client is
// configured to publish. It is a startup check, called once the client exists
// and before any deploy, and it hits the API server — so unlike Config.Validate
// it can only run against a real cluster.
func (c *Client) Preflight(ctx context.Context) error {
	if err := c.endpoints.preflight(ctx); err != nil {
		return fmt.Errorf("kube: preflight: %w", err)
	}
	return nil
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
