// Package schema generates the editor capability catalogue
// (packages/editor/src/app/schema/capabilities.json) from the block and
// connector metadata registered in package core. The output types below mirror
// packages/editor/src/app/schema/types.ts; the generated JSON must satisfy those
// TypeScript interfaces.
package schema

// FieldType is the union of editor field kinds (see types.ts FieldType).
type FieldType string

// FieldType values. Only the first four are produced by inference; every other
// kind must be declared explicitly with an octo `type=` clause. Named as
// constants so the walker and description overlay reference them without
// repeating string literals.
const (
	TypeString  FieldType = "string"
	TypeNumber  FieldType = "number"
	TypeBoolean FieldType = "boolean"
	TypeFlow    FieldType = "flow"
	TypeObject  FieldType = "object"
)

// validFieldTypes is the closed set of FieldType values an octo `type=` clause
// may name. Inference produces only a subset (string/number/boolean/…); the
// remainder must be declared explicitly.
var validFieldTypes = map[FieldType]bool{
	TypeString: true, TypeNumber: true, TypeBoolean: true, "cel": true, "enum": true,
	"string-list": true, "string-map": true, TypeFlow: true, "flow-list": true,
	"case-list": true, "route-list": true, "tool-list": true, "skill-list": true,
	"mcp-resource-list": true, "mcp-prompt-list": true, "block-list": true,
	TypeObject: true, "transform-list": true, "rule-list": true,
}

// Reference mirrors types.ts ReferenceSpec: a field that points at another named
// entity (a connector by type or category, a flow, or a template resource).
type Reference struct {
	Kind              string `json:"kind"`
	ConnectorType     string `json:"connectorType,omitempty"`
	ConnectorCategory string `json:"connectorCategory,omitempty"`
}

// ShowIf mirrors types.ts ShowIf: gate a field on a sibling's value.
type ShowIf struct {
	Field  string `json:"field"`
	Equals string `json:"equals"`
}

// Field mirrors types.ts FieldSpec. Field order here is the JSON key order the
// hand-written capabilities.json uses (showIf before description, ref and the
// nested fields after it), so generated entries match without reformatting. JSON
// objects are unordered, so this is purely for a clean diff.
type Field struct {
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Type        FieldType  `json:"type"`
	Required    bool       `json:"required"`
	Default     any        `json:"default,omitempty"`
	Enum        []string   `json:"enum,omitempty"`
	ShowIf      *ShowIf    `json:"showIf,omitempty"`
	Description string     `json:"description,omitempty"`
	Ref         *Reference `json:"ref,omitempty"`
	Fields      []Field    `json:"fields,omitempty"`
}

// Block mirrors types.ts BlockSpec.
type Block struct {
	Type        string  `json:"type"`
	Label       string  `json:"label"`
	Category    string  `json:"category"`
	Group       string  `json:"group,omitempty"`
	Icon        string  `json:"icon"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
}

// Source mirrors types.ts SourceSpec.
type Source struct {
	Type   string  `json:"type"`
	Label  string  `json:"label"`
	Icon   string  `json:"icon,omitempty"`
	Fields []Field `json:"fields"`
}

// Connector mirrors types.ts ConnectorSpec.
type Connector struct {
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Icon     string   `json:"icon,omitempty"`
	Category string   `json:"category,omitempty"`
	Settings []Field  `json:"settings"`
	Sources  []Source `json:"sources"`
}

// Capabilities mirrors types.ts Capabilities: the full catalogue.
type Capabilities struct {
	Blocks     []Block     `json:"blocks"`
	Connectors []Connector `json:"connectors"`
}
