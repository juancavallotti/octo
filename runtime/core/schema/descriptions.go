package schema

import "reflect"

// applyDescriptions overlays field descriptions onto an already-built field list.
// This is the no-op placeholder; the real implementation (parsing Go doc comments
// from the settings struct's source package) lands with the description overlay.
// A field that already carries a description from an octo desc= clause keeps it.
func applyDescriptions(_ []Field, _ string, _ reflect.Type) {}
