// Package k8s implements the runtime services provider for a Kubernetes cluster:
// leader election backed by coordination/v1 Leases (so work runs on one replica)
// and a two-tier KV store — the orchestrator API for persistent and secret
// namespaces (deployment-scoped, encrypted at rest), Redis for volatile ones. It
// self-registers as the "k8s" module; a binary blank-imports it to make it
// selectable via RUNTIME_SERVICES_MODULE=k8s.
package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/services"
	"github.com/nats-io/nats.go"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Module is this provider's name, matched against RUNTIME_SERVICES_MODULE.
const Module = "k8s"

// Environment variables the orchestrator injects into each runtime pod. POD_NAME
// and POD_NAMESPACE come from the downward API; the rest identify the deployment
// and the orchestrator KV endpoint.
const (
	envPodName        = "POD_NAME"
	envPodNamespace   = "POD_NAMESPACE"
	envDeploymentID   = "OCTO_DEPLOYMENT_ID"
	envDeploymentName = "OCTO_DEPLOYMENT_NAME"    // optional display name, stamped onto shipped logs
	envDeploymentVer  = "OCTO_DEPLOYMENT_VERSION" // optional tag/version, stamped onto shipped logs
	envSnapshotID     = "OCTO_SNAPSHOT_ID"        // optional snapshot the resources were frozen under; enables the loader
	envOrchestrator   = "ORCHESTRATOR_URL"
	// The credential this pod presents to the orchestrator. A deployment gets the
	// file: the orchestrator mints a token for it and mounts it, and a file can be
	// replaced without a restart and is not visible in the pod's spec to everyone
	// who can describe it. The variable is what a local run can set instead.
	envOrchestrTokenFile = "ORCHESTRATOR_TOKEN_FILE"
	envOrchestrToken     = "ORCHESTRATOR_TOKEN"
	// Where the pod renews its own token, since a short-lived one outlives neither
	// the pod nor the work it is doing. Absent, whatever was mounted stands until
	// it expires.
	envIAMURL = "IAM_URL"
	// The variable a flow reads to present this deployment's own identity. Not set
	// in the pod's environment by anybody: it is served from the credential above,
	// so that what an expression reads is the token that is valid now.
	envPlatformToken = "PLATFORM_TOKEN"
	envNATSURL       = "NATS_URL"  // NATS broker URL backing the queues
	envRedisURL      = "REDIS_URL" // optional Redis backing the volatile KV tier
)

func init() {
	services.Register(Module, New)
}

// podCredential builds this pod's platform credential from the environment and
// renews it once, so that what the pod presents is a token it obtained rather
// than one written before it started. start leaves a daemon behind to keep it
// that way.
//
// It then publishes the credential as `env.PLATFORM_TOKEN`, which is how a flow
// spends it. The variable is the whole interface: an integration triggered by a
// queue message or a webhook has no person behind it and no token of anybody
// else's to borrow, so this is the only credential it has — and because it is
// read through the seam rather than copied at load, what an expression gets is
// the current one rather than whatever was valid when the pod started.
//
// Registered only here. A runtime built without this provider has no such
// variable and does the ordinary lookup, so the same definition loads in the
// editor and runs under dolphin — and an operator running Octo elsewhere can set
// PLATFORM_TOKEN themselves and have it mean what it says.
func podCredential(ctx context.Context) *credential {
	cred := newCredential(credentialConfig{
		Seed:   os.Getenv(envOrchestrTokenFile),
		Inline: os.Getenv(envOrchestrToken),
		IAMURL: os.Getenv(envIAMURL),
	})
	cred.start(ctx)
	expr.RegisterEnvValue(envPlatformToken, cred.get)
	return cred
}

// Services is the Kubernetes runtime-services provider.
type Services struct {
	le        *leaderElection
	leases    *leases
	kv        *tieredStore
	q         *natsQueues
	t         *natsTopics
	conn      *nats.Conn
	logSink   slog.Handler
	traces    core.TracePublisher
	resources core.ResourceLoader
	memory    *agentMemory
}

// New builds the k8s provider from the in-cluster config and the orchestrator-
// injected environment. It fails when run outside a cluster or when a required
// variable is missing, so a misconfiguration surfaces at startup rather than on
// first use.
//
//nolint:ireturn // satisfies services.Factory (returns core.RuntimeServices)
func New(ctx context.Context, opts services.Options) (core.RuntimeServices, error) {
	identity := os.Getenv(envPodName)
	namespace := os.Getenv(envPodNamespace)
	deploymentID := os.Getenv(envDeploymentID)
	orchestrator := os.Getenv(envOrchestrator)
	natsURL := os.Getenv(envNATSURL)
	if err := requireEnv(map[string]string{
		envPodName:      identity,
		envPodNamespace: namespace,
		envDeploymentID: deploymentID,
		envOrchestrator: orchestrator,
		envNATSURL:      natsURL,
	}); err != nil {
		return nil, err
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("k8s: in-cluster config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s: clientset: %w", err)
	}

	conn, err := nats.Connect(natsURL, nats.Name("octo-runtime "+deploymentID))
	if err != nil {
		return nil, fmt.Errorf("k8s: connect nats %q: %w", natsURL, err)
	}

	cred := podCredential(ctx)

	volatile := volatileStore(deploymentID)

	// A tagged deploy carries the snapshot its definition and resources were frozen
	// under; the resource loader fetches those frozen resources from the orchestrator.
	// An untagged deploy has no snapshot, so resources stay no-op (nothing to load).
	var resources core.ResourceLoader = core.NoopResourceLoader{}
	if snapshotID := os.Getenv(envSnapshotID); snapshotID != "" {
		resources = newHTTPResourceLoader(orchestrator, snapshotID, cred)
	}

	slog.Info("k8s runtime services initialized",
		"identity", identity, "namespace", namespace, "deployment", deploymentID,
		"orchestrator", orchestrator, "nats", natsURL, "volatileKV", volatile != nil,
		"snapshot", os.Getenv(envSnapshotID) != "", "credential", cred.String())

	return &Services{
		le:     newLeaderElection(cs.CoordinationV1(), namespace, identity, deploymentID),
		leases: newLeases(cs.CoordinationV1(), namespace, identity, deploymentID, time.Now),
		kv: &tieredStore{
			persistent: newHTTPStore(orchestrator, deploymentID, cred),
			volatile:   volatile,
		},
		q:       newNATSQueues(conn, deploymentID),
		t:       newNATSTopics(conn, deploymentID),
		conn:    conn,
		logSink: newLogSink(conn, deploymentID, os.Getenv(envDeploymentName), os.Getenv(envDeploymentVer)),
		traces: newTracePublisher(conn, opts.Tracing, deploymentID,
			os.Getenv(envDeploymentName), os.Getenv(envDeploymentVer)),
		resources: resources,
		memory:    newAgentMemory(orchestrator, deploymentID, cred),
	}, nil
}

// volatileStore builds the Redis-backed volatile KV tier from REDIS_URL, or returns
// nil when there is none to build.
//
// Optional, like NATS_URL and unlike the orchestrator URL: a pod without one still
// runs, with volatile namespaces falling through to the orchestrator. A URL that
// does not parse is worth saying loudly — it is a configuration mistake rather than
// an absence — but it is not worth refusing to start over, for the same reason the
// absence is not: this tier's entire promise is that losing it is survivable.
func volatileStore(deploymentID string) *redisStore {
	url := os.Getenv(envRedisURL)
	if url == "" {
		slog.Warn("k8s: no " + envRedisURL + " was injected; volatile objects will be stored " +
			"in the orchestrator database like persistent ones")
		return nil
	}
	store, err := newRedisStore(url, deploymentID)
	if err != nil {
		slog.Error("k8s: volatile KV is misconfigured, falling back to the orchestrator "+
			"for volatile namespaces", "error", err)
		return nil
	}
	return store
}

// LeaderElection returns the Lease-based leader election.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) LeaderElection() core.LeaderElection { return s.le }

// Leases returns the fail-fast claims, backed by coordination Lease objects in
// the deployment's namespace. It shares the API group with leader election above
// and answers a different question — see core.Leases.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Leases() core.Leases { return s.leases }

// KV returns the orchestrator-backed key/value store.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) KV() core.KV { return s.kv }

// Secrets routes through the same KV store to the encrypted secret namespaces.
// Those are never volatile, so a secret always takes the orchestrator path and is
// always encrypted at rest — the tiered store dispatches on the namespace, and a
// secret namespace never names the volatile tier.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Secrets() core.SecretStore { return core.NewSecretStore(s.kv) }

// Queues returns the NATS-backed message queues.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Queues() core.Queues { return s.q }

// Topics returns the NATS-backed broadcast pub/sub.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Topics() core.Topics { return s.t }

// Resources returns the resource loader: for a tagged deploy it fetches the
// resources frozen under the deployment's snapshot from the orchestrator; for an
// untagged deploy (no snapshot) it is the no-op loader, so every resource reports
// missing.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Resources() core.ResourceLoader { return s.resources }

// LogSink returns the handler that ships log records to the shared internal.logs
// subject, satisfying core.LogShipper so the runtime tees its loggers through it.
//
//nolint:ireturn // satisfies core.LogShipper
func (s *Services) LogSink() slog.Handler { return s.logSink }

// Traces returns the module's trace publisher: records go to the shared
// internal.traces subject for a traces engine to consume, tagged with the
// deployment they came from.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Traces() core.TracePublisher { return s.traces }

// AgentMemory returns the module's agent-memory store: the orchestrator's
// database, over the same deployment-scoped HTTP surface KV uses.
//
// Keyed on the integration rather than the deployment, which is why a
// conversation survives an undeploy-then-deploy where kv_store's rows do not.
//
//nolint:ireturn // satisfies the RuntimeServices interface
func (s *Services) AgentMemory() core.AgentMemory { return s.memory }

// Close releases the store client's idle connections and the NATS connection.
// Leader-election campaigns are bound to the context passed to Acquire and stop
// when the runtime stops.
func (s *Services) Close() error {
	s.memory.close()
	if err := s.kv.close(); err != nil {
		slog.Error("k8s: closing the KV store", "error", err)
	}
	if c, ok := s.resources.(interface{ close() }); ok {
		c.close()
	}
	// Drain the trace publisher before the connection it publishes on: its Close
	// waits for what is queued to reach the server, which a closed connection
	// would make impossible.
	//
	// Its error is reported rather than swallowed — it means records this pod
	// saw never reached the broker, which nothing downstream can infer — but it
	// does not stop the connection from closing: a shutdown that leaves a socket
	// open because telemetry failed has made the problem worse.
	var traceErr error
	if closer, ok := s.traces.(interface{ Close() error }); ok {
		traceErr = closer.Close()
	}
	s.conn.Close()
	return traceErr
}

// requireEnv returns an error naming every variable that is empty.
func requireEnv(vars map[string]string) error {
	var missing []string
	for name, value := range vars {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("k8s: missing required environment: %v", missing)
}
