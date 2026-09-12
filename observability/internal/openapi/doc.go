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
//	@description				Every route requires a bearer token minted by this platform's iam
//	@description				service, except the health and description endpoints. What a route
//	@description				requires of the caller depends on the roles the token carries: the
//	@description				stored history and the alerting view are open to anyone signed in,
//	@description				while what this installation watches for, how long it keeps what it
//	@description				stores, and what that storage costs are administrators only.
//	@description
//	@description				An installation with no iam configured authorizes nothing and serves
//	@description				every caller, which is what a local run is.
//	@license.name				Elastic License 2.0
//	@license.url				https://www.elastic.co/licensing/elastic-license
//	@externalDocs.description	Octo documentation
//	@externalDocs.url			https://octopaas.dev/docs
//
//	@securityDefinitions.apikey	Bearer
//	@in							header
//	@name						Authorization
package openapi
