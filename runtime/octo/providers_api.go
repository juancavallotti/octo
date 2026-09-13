//go:build api

package main

// Built with -tags api: ship only the provider that delegates every platform
// capability — KV, secrets, resources, leases, leader election, queues, topics,
// agent memory, traces and logs — to an HTTP API the operator implements, named
// by OCTO_PLATFORM_API_URL.
//
// Whatever implements that API, this is the same binary; only the URL differs. It
// carries none of the cluster dependencies, because everything it needs is
// net/http. The tag is here for what it excludes rather than what it costs: a
// binary that could select two providers is one whose deployment can be
// misconfigured into talking to the wrong platform.
import _ "github.com/juancavallotti/octo/runtime/services/api" // registers the "api" services provider
