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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Module is this provider's name, matched against RUNTIME_SERVICES_MODULE.
const Module = "k8s"

// Environment variables the orchestrator injects into each runtime pod. POD_NAME
// and POD_NAMESPACE come from the downward API; the rest identify the deployment
// and the orchestrator KV endpoint.
const (
	envPodName       = "POD_NAME"
	envPodNamespace  = "POD_NAMESPACE"
	envDeploymentID  = "OCTO_DEPLOYMENT_ID"
	envOrchestrator  = "ORCHESTRATOR_URL"
	envOrchestrToken = "ORCHESTRATOR_TOKEN" // optional bearer token for the KV API
)

func init() {
	services.Register(Module, New)
}

// Services is the Kubernetes runtime-services provider.
type Services struct {
	le *leaderElection
	kv *httpStore
}

// New builds the k8s provider from the in-cluster config and the orchestrator-
// injected environment. It fails when run outside a cluster or when a required
// variable is missing, so a misconfiguration surfaces at startup rather than on
// first use.
//
//nolint:ireturn // satisfies services.Factory (returns core.RuntimeServices)
func New(_ context.Context) (core.RuntimeServices, error) {
	identity := os.Getenv(envPodName)
	namespace := os.Getenv(envPodNamespace)
	deploymentID := os.Getenv(envDeploymentID)
	orchestrator := os.Getenv(envOrchestrator)
	if err := requireEnv(map[string]string{
		envPodName:      identity,
		envPodNamespace: namespace,
		envDeploymentID: deploymentID,
		envOrchestrator: orchestrator,
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

	slog.Info("k8s runtime services initialized",
		"identity", identity, "namespace", namespace, "deployment", deploymentID, "orchestrator", orchestrator)

	return &Services{
		le: newLeaderElection(cs.CoordinationV1(), namespace, identity, deploymentID),
		kv: newHTTPStore(orchestrator, deploymentID, os.Getenv(envOrchestrToken)),
	}, nil
}

//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) LeaderElection() core.LeaderElection { return s.le }

//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) KV() core.KV { return s.kv }

// Secrets routes through the same KV store to the encrypted secret namespaces.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Secrets() core.SecretStore { return core.NewSecretStore(s.kv) }

// Close releases the store client's idle connections. Leader-election campaigns are
// bound to the context passed to Acquire and stop when the runtime stops.
func (s *Services) Close() error {
	s.kv.close()
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
