// Schema-only metadata for composite (control-flow) blocks. Composites are not
// leaf blocks: they are dispatched by the flow builder (flow.go) and read their
// typed sub-flow slots off types.BlockConfig, so they have no settings struct to
// reflect. Instead each declares a schema-only meta struct here — never decoded,
// only reflected for the editor schema — whose fields carry octo slot tags. A
// pure sub-flow slot uses a *struct{} placeholder since type=flow overrides
// inference and no value is read from it.
package engine

import (
	"reflect"

	"github.com/juancavallotti/octo/core"
)

func init() {
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "if",
		Label:       "If",
		Category:    "control-flow",
		Group:       "Flow Control",
		Icon:        "Split",
		Description: "Conditional branching on a CEL boolean expression.",
		Config:      reflect.TypeFor[ifMeta](),
	})
}

// ifMeta is the schema-only description of the `if` composite's editor slots.
type ifMeta struct {
	// CEL boolean expression.
	Condition string `json:"condition" octo:"label=Condition,type=cel,required"`
	// Flow run when the condition is true.
	Then *struct{} `json:"then" octo:"label=Then,type=flow,required"`
	// Flow run when the condition is false.
	Else *struct{} `json:"else" octo:"label=Else,type=flow"`
}
