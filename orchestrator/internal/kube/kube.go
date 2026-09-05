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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
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

	// defaultWorkspaceSize caps the agentic runner's /workspace when the chart
	// names no size. Small on purpose: the workspace is scratch — a definition
	// being drafted, a test suite, a report — and a bound that a runaway command
	// hits quickly is more useful than one it takes an hour to reach.
	defaultWorkspaceSize = "100Mi"
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
	// RedisURL is the in-cluster Redis backing the volatile KV tier, injected as a
	// literal. Empty omits it, and the runtime then stores volatile objects through
	// the orchestrator like persistent ones.
	RedisURL string
	// RedisSecret names a Secret holding the Redis URL instead, for a managed Redis
	// whose URL carries a password. A password written as a literal into every
	// integration Deployment would be readable by anyone who can read workloads,
	// which is a wider audience than anyone who can read Secrets — the same
	// reasoning the chart's octo.redis.env helper spells out for the platform's own
	// pods. When set it wins over RedisURL.
	RedisSecret SecretKeyRef
	// LogsURL is the log aggregator's query API. Unlike the others it is injected
	// only into deployments that were granted the observability API, so an empty
	// value here disables the grant everywhere rather than degrading every pod.
	LogsURL string
	// EmbeddingsURL is the embedding server: text in, vectors out. Empty omits it,
	// which is what an installation with no embedding server has.
	//
	// Injected into EVERY pod, unlike LogsURL beside it, and the difference is the
	// point. LOGS_URL is a grant because stored telemetry is other integrations'
	// data and handing it out would cross a boundary. An embedding crosses none: it
	// reads nothing, writes nothing, and costs a fraction of a cent. Giving every
	// pod the URL is what makes it unnecessary to give any pod the provider API
	// key, which is the trade this server exists to make.
	EmbeddingsURL string
}

// SecretKeyRef names one key in one Secret in the release namespace. Integration
// pods run there too, so a reference the chart resolved for the orchestrator
// resolves for them unchanged.
type SecretKeyRef struct {
	Name string
	Key  string
}

// set reports whether the reference names something.
func (s SecretKeyRef) set() bool { return s.Name != "" && s.Key != "" }

// Client wraps a Kubernetes clientset scoped to one namespace and runtime image.
type Client struct {
	clientset    kubernetes.Interface
	namespace    string
	runtimeImage string
	// runtimeVersion is which octo runtimeImage IS, when the reference itself
	// cannot say — a digest-pinned image names no tag. Supplied by the chart from
	// the same release the image was built for. Empty outside a chart install,
	// where the tag on the reference is the answer.
	runtimeVersion string
	// devRuntimeImage is the STANDALONE runtime build, used by dev-run pods. Distinct
	// from runtimeImage on purpose: that one is built -tags k8s and contains only the
	// k8s services provider, so it cannot run without the orchestrator, the cluster
	// queues and the log aggregator — none of which a dev run should be wired to.
	devRuntimeImage string
	// agenticRunnerImage is the runner a deployment gets when it asks for
	// RunnerAgentic: the same k8s runtime as runtimeImage, on a base that also
	// carries a shell, curl, jq, the standalone octo CLI and dolphin. Empty means
	// the runner is not configured on this install, and an agentic deploy is
	// refused rather than quietly served the distroless image — see RunnerEnabled.
	agenticRunnerImage string
	// agenticResources sizes the agentic runner's container. Only that runner's,
	// deliberately: giving every integration pod requests and limits from one
	// cluster-wide setting would change the scheduling of every deployment that
	// already exists, which is its own feature and not this one.
	agenticResources corev1.ResourceRequirements
	// workspaceSize caps the agentic runner's /workspace emptyDir. A cap and not an
	// allocation: the volume costs nothing until it is written to, and the limit is
	// what stops a runaway command filling the node's disk.
	workspaceSize resource.Quantity
	// sidecarImage runs beside that runtime in a dev-run pod and owns its workspace.
	sidecarImage string
	// sidecarPort is where that sidecar serves its command API.
	sidecarPort int32
	// statsSidecar configures the pod stats sidecar injected into deployed
	// integration pods. Zero value means no deployment gains one.
	statsSidecar StatsSidecar
	// orchestratorURL is this orchestrator's own in-cluster address, injected into a
	// dev-run sidecar so it can pull its bundle. Distinct from
	// runtimeServices.OrchestratorURL, which is documented as the KV API a deployed
	// runtime reaches — the same host, but a different reason to know it.
	orchestratorURL string
	baseDomain      string // parent domain for external endpoints ("" = disabled)
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

// Runner names the image a deployment's pods run.
//
// There are two because an integration's needs genuinely differ in kind, not in
// degree. Almost every one wants the smallest possible thing that can run a flow,
// and gets it: a distroless image with one static binary, no shell and nothing
// writable. A few are built to *drive* the platform rather than serve it — they
// run local commands, invoke flows, or execute a test suite — and for those the
// distroless image is not merely minimal, it is empty of the programs the flow
// names. Handing every deployment the larger image to spare those few would be
// the wrong trade in exactly the place it matters most.
type Runner string

const (
	// RunnerStandard is the generic octo-runtime image every integration gets:
	// distroless, one binary, nothing else. The zero value, deliberately.
	RunnerStandard Runner = "standard"
	// RunnerAgentic is the heavier image that also carries a shell, curl, jq, the
	// standalone octo CLI and dolphin — for a deployment whose flows run local
	// commands or test other flows. Dr. Octo is the first of them.
	RunnerAgentic Runner = "agentic"
)

// ParseRunner converts a configured value into a Runner. Empty means the default.
// An unrecognised value is an error rather than a fallback, for the same reason
// ParseEndpointAPI refuses one: `runner: "agentik"` silently deploying the
// distroless image would produce a pod whose every command fails with "not
// found", and nothing anywhere would call that a configuration mistake.
func ParseRunner(s string) (Runner, error) {
	switch Runner(s) {
	case "":
		return RunnerStandard, nil
	case RunnerStandard:
		return RunnerStandard, nil
	case RunnerAgentic:
		return RunnerAgentic, nil
	default:
		return "", fmt.Errorf("kube: unknown runner %q (want %q or %q)", s, RunnerStandard, RunnerAgentic)
	}
}

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
	Namespace    string
	RuntimeImage string
	// RuntimeVersion is the release RuntimeImage is, for the digest-pinned case
	// where the reference carries no tag to read.
	RuntimeVersion string
	// AgenticRunnerImage is the RunnerAgentic image; "" disables that runner.
	AgenticRunnerImage string
	// AgenticRunnerResources sizes its container; the zero value sets none, which
	// is what every deployment gets today.
	AgenticRunnerResources corev1.ResourceRequirements
	// AgenticRunnerWorkspaceSize caps its /workspace volume; "" applies
	// defaultWorkspaceSize.
	AgenticRunnerWorkspaceSize string
	DevRuntimeImage            string
	SidecarImage               string
	SidecarPort                int32
	// StatsSidecar configures the pod stats sidecar. Its zero value leaves every
	// deployment exactly as it is today.
	StatsSidecar      StatsSidecar
	OrchestratorURL   string
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
	if s := c.AgenticRunnerWorkspaceSize; s != "" {
		q, err := resource.ParseQuantity(s)
		if err != nil {
			return fmt.Errorf("kube: agentic runner workspace size %q is not a quantity (e.g. 100Mi): %w", s, err)
		}
		if q.Sign() <= 0 {
			return fmt.Errorf(
				"kube: agentic runner workspace size %q must be positive — kubelet reads a zero "+
					"limit as no limit, which is the unbounded workspace this setting prevents", s)
		}
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
		clientset:          cs,
		namespace:          cfg.Namespace,
		runtimeImage:       cfg.RuntimeImage,
		runtimeVersion:     cfg.RuntimeVersion,
		agenticRunnerImage: cfg.AgenticRunnerImage,
		agenticResources:   cfg.AgenticRunnerResources,
		workspaceSize:      parseWorkspaceSize(cfg.AgenticRunnerWorkspaceSize),
		devRuntimeImage:    cfg.DevRuntimeImage,
		sidecarImage:       cfg.SidecarImage,
		sidecarPort:        cfg.SidecarPort,
		statsSidecar:       cfg.StatsSidecar,
		orchestratorURL:    cfg.OrchestratorURL,
		baseDomain:         cfg.BaseDomain,
		imagePullSecrets:   cfg.ImagePullSecrets,
		runtimeServices:    cfg.RuntimeServices,
	}
	if c.sidecarPort == 0 {
		c.sidecarPort = defaultSidecarPort
	}
	// The two are the same host in every real deployment, so falling back keeps a
	// caller that only wired the runtime-services URL working rather than silently
	// producing sidecars with nowhere to pull from.
	if c.orchestratorURL == "" {
		c.orchestratorURL = cfg.RuntimeServices.OrchestratorURL
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

// DevRunsEnabled reports whether dev runs can be created. Checked by the caller at
// startup so the feature is disabled with a log line, rather than surfacing later as
// pods that fail in a way nobody connects back to configuration.
//
// All three parts are required, and each one's absence produces a pod that fails
// rather than a feature that degrades: without the standalone runtime image there is
// nothing to run the integration, without the sidecar image nothing populates the
// workspace, and without the orchestrator URL the sidecar refuses to start at all
// (it treats a missing ORCHESTRATOR_URL as a hard failure, correctly, since it would
// otherwise sit there healthy and never pull). Reporting it here turns three
// different CrashLoopBackOffs into one startup log line.
func (c *Client) DevRunsEnabled() bool {
	return c.devRuntimeImage != "" && c.sidecarImage != "" && c.orchestratorURL != ""
}

// RunnerEnabled reports whether a runner can be deployed on this install.
//
// The standard runner always can — it is the image the orchestrator has always
// used. The agentic one needs a chart that configures it, and a deployment that
// asks for one it cannot have is refused up front with the setting named. The
// alternative, falling back to the standard image, is the failure this exists to
// prevent: the pod comes up healthy, and then every command the flow runs fails
// with "not found" — a symptom that points at the flow rather than at the chart.
func (c *Client) RunnerEnabled(r Runner) bool {
	if r == RunnerAgentic {
		return c.agenticRunnerImage != ""
	}
	return true
}

// RuntimeVersion is the release the deployed runtime image is, as configured.
// Empty when nothing said, which is the signal to fall back to whatever tag the
// image reference carries.
//
// It is not per-runner: the standard runtime and the agentic runner are built
// from one release and shipped together, so a deployment on either is on that
// release.
func (c *Client) RuntimeVersion() string { return c.runtimeVersion }

// RunnerImage is the image a spec's runner runs. Callers reach it only after
// RunnerEnabled has said yes, so an unconfigured agentic runner cannot arrive
// here; falling through to the standard image is the safe answer for the zero
// value and for a Runner that somehow bypassed ParseRunner.
//
// Exported because a deploy records the image it shipped in the deployment's
// metadata: which runtime a workload was put on is a fact about that deploy, and
// the cluster stops being able to answer it the moment the workload is gone.
func (c *Client) RunnerImage(r Runner) string {
	if r == RunnerAgentic && c.agenticRunnerImage != "" {
		return c.agenticRunnerImage
	}
	return c.runtimeImage
}

// parseWorkspaceSize turns the configured workspace cap into a Quantity, falling
// back to the default when it is unset or unparseable.
//
// Unparseable falls back rather than failing, which is the opposite of how this
// package treats an unknown endpoint API or runner — and for a reason worth
// stating. Those name a thing that either exists or does not, so a typo means the
// operator asked for something impossible. This is a bound on scratch space: a
// mistyped one still wants a bound, and the default is a better answer than
// refusing to start the orchestrator over the size of a temp directory. Config
// .Validate reports it, so the mistake is still said out loud.
func parseWorkspaceSize(s string) resource.Quantity {
	if s != "" {
		// Positive, not merely parseable. "0" is a valid Quantity and kubelet reads a
		// zero SizeLimit as NO limit, so a chart that set it would silently get the
		// unbounded workspace this field exists to prevent — the one where a runaway
		// command fills the node's disk and evicts every pod on it, not just this one.
		if q, err := resource.ParseQuantity(s); err == nil && q.Sign() > 0 {
			return q
		}
	}
	return resource.MustParse(defaultWorkspaceSize)
}

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
