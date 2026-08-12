// Package openapi serves the log aggregator's own API description.
//
// The document is generated from the annotations on the query handlers by
// `task logs:openapi` and committed as swagger.json beside this file, then embedded
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
//	@title						Octo Logs API
//	@version					1.0
//	@description				The read side of the platform's telemetry: log events and the traces
//	@description				beside them, as shipped by deployed runtimes and stored in Postgres.
//	@description
//	@description				The service is called "logs" everywhere — the binary, the image, the pod
//	@description				and the LOGS_URL other services reach it by — but it stores traces too.
//	@description				They landed here because they are the same shape of problem, so one
//	@description				description covers both.
//	@description
//	@description				This is an in-cluster service and performs no authentication of its own.
//	@description				It is ClusterIP in the chart and the platform BFF is the authorization
//	@description				boundary in front of it. Every route is a read; nothing here writes.
//	@license.name				Elastic License 2.0
//	@license.url				https://www.elastic.co/licensing/elastic-license
//	@externalDocs.description	Octo documentation
//	@externalDocs.url			https://octopaas.dev/docs
package openapi
