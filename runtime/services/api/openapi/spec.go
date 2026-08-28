package openapi

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ClientSpecVersion is the contract version the runtime speaks, and the version
// the document declares. It lives here, in the data-only package, so the client
// and the document read it from one place — two copies of a version string
// disagree the first time either moves.
const ClientSpecVersion = "1.0"

// specYAML is the contract, embedded so a binary carries the document it speaks.
//
//go:embed octo-platform-api.yaml
var specYAML []byte

// Spec returns the contract as OpenAPI 3.1 YAML, byte for byte as it is written.
//
// Verbatim rather than re-encoded, because the comments in it are half the
// document: they say why a route is shaped the way it is, and an implementer
// reading the contract is exactly who they are for. Round-tripping through a YAML
// encoder would drop every one of them.
func Spec() []byte { return specYAML }

// SpecJSON returns the same document as JSON, for tooling that will not read
// YAML. It is converted rather than kept as a second file: two copies of a
// contract are one edit away from disagreeing.
func SpecJSON() ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(specYAML, &doc); err != nil {
		return nil, fmt.Errorf("openapi: read the platform API contract: %w", err)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("openapi: convert the platform API contract to JSON: %w", err)
	}
	return append(out, '\n'), nil
}
