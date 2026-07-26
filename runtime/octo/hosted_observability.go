//go:build !no_observability

package main

// The observability runtime service is compiled in by default, on every distro:
// probes a deployment has to enable are probes most deployments will not have.
// Build with -tags no_observability to drop it — and the prometheus client it
// brings — from a binary that has no use for it.
import _ "github.com/juancavallotti/octo/runtime/services/observability" // registers the "observability" hosted service
