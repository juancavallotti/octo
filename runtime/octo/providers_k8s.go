//go:build k8s

package main

// Built with -tags k8s (the cluster image the orchestrator deploys): ship only
// the k8s services provider — Lease-based leader election, the orchestrator KV,
// and NATS-backed queues. The standalone provider is the default build (see
// providers_standalone.go), so the two images stay disjoint. The orchestrator
// always sets RUNTIME_SERVICES_MODULE=k8s for deployed pods.
import _ "github.com/juancavallotti/octo/runtime/services/k8s" // registers the "k8s" services provider
