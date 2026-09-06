package engine

import (
	"errors"

	"github.com/juancavallotti/octo/runtime/core"
)

// This file holds what the debug blocks — breakpoint, spy — have in common. They
// are injected by the runtime around a block the user addressed from the CLI, never
// authored in a flow, and they are deliberately absent from the schema registry
// (RegisterBlockMeta): they must never appear in the editor palette, in `octo
// schema`, or in the docs. Registering them there would also fail the docs-drift
// check, which requires every schema-visible type to be documented on a reference
// page.

// unlabel strips the block label a debug wrapper's sub-flow added, so the enclosing
// flow's own labelling lands exactly where it would have without the wrapper.
//
// Both flows label: the sub-flow tags the error with the wrapped block's label on
// the way out, and the enclosing flow tags the wrapper with the same label (the
// injector copies it). Left alone that reads `block "x": block "x": ...` and the
// wrapper would show through. Only the outermost layer is removed, so a wrapped
// composite keeps the nesting it produces on its own.
func unlabel(err error) error {
	var labelled *core.BlockError
	if errors.As(err, &labelled) {
		return labelled.Err
	}
	return err
}
