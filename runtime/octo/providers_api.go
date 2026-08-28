//go:build api

package main

// Built with -tags api: ship only the provider that delegates every platform
// capability — KV, secrets, resources, leases, leader election, queues, topics,
// agent memory, traces and logs — to an HTTP API the operator implements, named
// by OCTO_PLATFORM_API_URL.
//
// This is the flavour for running Octo outside our own PaaS: a Cloud Run service
// over Firestore and Pub/Sub, a central platform service that Kubernetes pods
// call, or a sidecar on loopback. All three are the same binary and differ only
// in what that URL points at.
//
// It carries none of the cluster dependencies — no client-go, no NATS — because
// everything it needs is net/http. The tag is here for what it excludes rather
// than what it costs: a binary that could select two providers is a binary whose
// deployment can be misconfigured into talking to the wrong platform.
import _ "github.com/juancavallotti/octo/runtime/services/api" // registers the "api" services provider
