//go:build !no_observability

package main

// The observability runtime service is compiled in by default, on every distro:
// probes a deployment has to enable are probes most deployments will not have.
// Build with -tags no_observability to drop it — and the prometheus client it
// brings — from a binary that has no use for it.
//
// This file is also where anything the service needs from package main is handed
// over, so that coupling sits behind the same build tag as the service itself.
import "github.com/juancavallotti/octo/runtime/services/observability"

func init() {
	// The version is a const in package main, which a runtime service cannot
	// import back; octo_build_info would otherwise say "unknown".
	observability.SetBuildInfo(Version, BuildDate)
}
