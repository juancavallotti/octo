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
//	@description				Every route requires a bearer token minted by this platform's iam
//	@description				service, except the health and description endpoints. What a route
//	@description				requires of the caller depends on the roles the token carries: most
//	@description				reads are open to anyone signed in, building and deploying need the
//	@description				corresponding role, and the installation's own settings and secrets
//	@description				are administrators only. A deployed integration presents a token of
//	@description				its own, which reaches its key-value store, its objects and its agent
//	@description				memory and nothing else.
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
