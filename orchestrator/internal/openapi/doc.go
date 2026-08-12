// Package openapi serves the orchestrator's own API description.
//
// The document is generated from the annotations on the feature handlers by
// `task orchestrator:openapi` and committed as swagger.json beside this file, then
// embedded into the binary. Generation is strictly a build-time step: nothing here
// parses Go source at runtime, and the swaggo toolchain is not a dependency of the
// module — only of the task that regenerates the artifact. CI regenerates and fails
// on a diff, so the committed spec and the annotations cannot drift apart.
//
// It exists because a description of this API is the difference between a client
// that has to be told every route and one that can look them up — the platform
// agent being the first such client, but not the only conceivable one.
//
// The general API annotations live here rather than in main.go so that file stays
// about wiring.
//
//	@title						Octo Orchestrator API
//	@version					1.0
//	@description				The control plane behind the Octo platform: integrations and their
//	@description				resources, version tags, deployments, cluster secrets, the
//	@description				deployment-scoped key-value store, users and API keys, and the
//	@description				site-wide settings.
//	@description
//	@description				The orchestrator is an in-cluster service and performs no
//	@description				authentication of its own. Callers reach it over the cluster network
//	@description				and the platform BFF is the authorization boundary in front of it.
//	@description				Identity, where a route needs it, is passed as a parameter (an actor
//	@description				id, a user id in the path) rather than proven by a credential.
//	@license.name				Elastic License 2.0
//	@license.url				https://www.elastic.co/licensing/elastic-license
//	@externalDocs.description	Octo documentation
//	@externalDocs.url			https://octopaas.dev/docs
package openapi
