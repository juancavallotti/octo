//go:build !k8s && !api

package main

// The default build ships only the standalone services provider, keeping the
// cluster dependencies (Kubernetes client-go, NATS) out of the binary. This is
// the standalone "try Octo" image and what local builds and tests use.
//
// The other two providers are compiled in only under their own tags: -tags k8s
// for the cluster provider (providers_k8s.go) and -tags api for the one that
// delegates every capability to an operator-implemented HTTP API
// (providers_api.go). The constraint above names both, so exactly one provider
// is ever present and a binary can never be ambiguous about which platform it
// is talking to.
import _ "github.com/juancavallotti/octo/runtime/services/standalone" // registers the "standalone" services provider (default)
