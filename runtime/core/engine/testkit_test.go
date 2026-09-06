package engine

import (
	"github.com/juancavallotti/octo/runtime/internal/testkit"

	// The engine owns no blocks, so its tests borrow the control-flow ones to
	// exercise sub-flows, addresses, events and stops through real composites.
	_ "github.com/juancavallotti/octo/runtime/blocks/controlflow"
)

// The shared fixtures, under the names this package's tests grew up with.
type processorFunc = testkit.ProcessorFunc

var (
	testRegistry    = testkit.Registry
	inheritRegistry = testkit.Inherit
	mustMessage     = testkit.Message
)
