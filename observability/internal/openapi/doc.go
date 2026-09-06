// Package openapi serves the observability service's own API description.
//
// The document is generated from the annotations on the query handlers by
// `task observability:openapi` and committed as swagger.json beside this file, then embedded
// into the binary. Generation is strictly a build-time step: nothing here parses Go
// source at runtime, and the swaggo toolchain is not a dependency of the module —
// only of the task that regenerates the artifact. CI regenerates and fails on a
// diff, so the committed spec and the annotations cannot drift apart.
//
// It is the orchestrator's openapi package again, deliberately copied rather than
// shared. This module depends on pgx and nats and nothing else; importing the
// orchestrator to reuse a hundred lines of JSON walking would tie two services
// together that are otherwise independent, which is a worse trade than the
// duplication. If a third service wants this, that is the moment to reconsider.
//
// The general API annotations live here rather than in main.go so that file stays
// about wiring.
//
//	@title						Octo Observability API
//	@version					1.0
//	@description				The platform's observability surface: stored log events and traces as
//	@description				shipped by deployed runtimes, the pod stats the stats sidecar collects,
//	@description				the retention policy over what is kept, and a report on how full the
//	@description				two stores underneath are.
//	@description
//	@description				This is an in-cluster service and performs no authentication of its own.
//	@description				It is ClusterIP in the chart and the platform BFF is the authorization
//	@description				boundary in front of it. Every query route is a read; the three
//	@description				retention routes are the exception, and one of them deletes stored
//	@description				telemetry — so anything that can reach this address can erase it as
//	@description				well as read it. Keep it internal.
//	@license.name				Elastic License 2.0
//	@license.url				https://www.elastic.co/licensing/elastic-license
//	@externalDocs.description	Octo documentation
//	@externalDocs.url			https://octopaas.dev/docs
package openapi
