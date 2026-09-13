// Package openapi holds the platform API contract the api services module
// expects, as an OpenAPI 3.1 document, and serves it to whoever needs to read it.
//
// The document is HAND-WRITTEN, and has to be: it describes an API this repo does
// not implement and never will — the runtime is the client, the server is somebody
// else's — so there are no handlers to generate it from. It is YAML because people
// edit and read it, and it needs comments, multi-line descriptions and examples.
//
// Because generation cannot check it, tests do. routes_test.go asserts the
// document's paths are exactly the route table the module calls, and
// discovery_test.go asserts the discovery schema's property names match the Go
// structs field for field, each failing with a message saying what drifted.
//
// The package is data only: no init, no registration, no build tag. It is compiled
// into every octo binary, so the contract a user reads is the one their runtime
// actually speaks.
package openapi
