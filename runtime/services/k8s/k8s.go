// Package k8s implements the runtime services provider for a Kubernetes cluster:
// leader election backed by coordination/v1 Leases (so work runs on one replica)
// and a KV store backed by the orchestrator API (deployment-scoped, with encrypted
// secrets). It self-registers as the "k8s" module; a binary blank-imports it to
// make it selectable via RUNTIME_SERVICES_MODULE=k8s.
package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/services"
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
	envOrchestrator   = "ORCHESTRATOR_URL"
	envOrchestrToken  = "ORCHESTRATOR_TOKEN" // optional bearer token for the KV API
	envNATSURL        = "NATS_URL"           // NATS broker URL backing the queues
)

func init() {
	services.Register(Module, New)
}

// Services is the Kubernetes runtime-services provider.
type Services struct {
	le      *leaderElection
	kv      *httpStore
	q       *natsQueues
	t       *natsTopics
	conn    *nats.Conn
	logSink slog.Handler
}

// New builds the k8s provider from the in-cluster config and the orchestrator-
// injected environment. It fails when run outside a cluster or when a required
// variable is missing, so a misconfiguration surfaces at startup rather than on
// first use.
//
//nolint:ireturn // satisfies services.Factory (returns core.RuntimeServices)
func New(_ context.Context, _ services.Options) (core.RuntimeServices, error) {
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

	slog.Info("k8s runtime services initialized",
		"identity", identity, "namespace", namespace, "deployment", deploymentID,
		"orchestrator", orchestrator, "nats", natsURL)

	return &Services{
		le:      newLeaderElection(cs.CoordinationV1(), namespace, identity, deploymentID),
		kv:      newHTTPStore(orchestrator, deploymentID, os.Getenv(envOrchestrToken)),
		q:       newNATSQueues(conn, deploymentID),
		t:       newNATSTopics(conn, deploymentID),
		conn:    conn,
		logSink: newLogSink(conn, deploymentID, os.Getenv(envDeploymentName), os.Getenv(envDeploymentVer)),
	}, nil
}

// LeaderElection returns the Lease-based leader election.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) LeaderElection() core.LeaderElection { return s.le }

// KV returns the orchestrator-backed key/value store.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) KV() core.KV { return s.kv }

// Secrets routes through the same KV store to the encrypted secret namespaces.
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

// Resources returns the no-op resource loader: k8s-backed resources (ConfigMaps,
// a template store) are implemented in a later PR, so every resource reports
// missing for now.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

// LogSink returns the handler that ships log records to the shared internal.logs
// subject, satisfying core.LogShipper so the runtime tees its loggers through it.
//
//nolint:ireturn // satisfies core.LogShipper
func (s *Services) LogSink() slog.Handler { return s.logSink }

// Close releases the store client's idle connections and the NATS connection.
// Leader-election campaigns are bound to the context passed to Acquire and stop
// when the runtime stops.
func (s *Services) Close() error {
	s.kv.close()
	s.conn.Close()
	return nil
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
