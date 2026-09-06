// Package builtin holds the leaf blocks every runtime ships that belong to no
// connector: the message setters (set-payload, set-variable, delete-variable),
// multi-transform, the object store blocks, template-resource and flow-ref. They
// own no resource and bind to whatever the runtime provides through BlockDeps.
package builtin

// Palette groups the blocks fall under in the editor sidebar.
const (
	groupData         = "Data"
	groupStorageCache = "Storage & Cache"
)

// init is this module's manifest: the one place that says what the package puts
// into the block registry and the editor schema, in a deterministic order.
func init() {
	registerSetters()
	registerMultiTransform()
	registerObjectBlocks()
	registerTemplateResource()
	registerFlowRef()
}
