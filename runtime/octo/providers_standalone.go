//go:build !k8s

package main

// The default build ships only the standalone services provider, keeping the
// cluster dependencies (Kubernetes client-go, NATS) out of the binary. This is
// the standalone "try Octo" image and what local builds and tests use. The k8s
// provider is compiled in only with -tags k8s (see providers_k8s.go).
import _ "github.com/juancavallotti/octo/runtime/services/standalone" // registers the "standalone" services provider (default)
