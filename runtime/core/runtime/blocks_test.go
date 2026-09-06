package runtime

import (
	// The runtime's tests build flows out of the first-party blocks, which live
	// in their own packages and register on the default registry when imported.
	_ "github.com/juancavallotti/octo/runtime/blocks/ai"
	_ "github.com/juancavallotti/octo/runtime/blocks/builtin"
	_ "github.com/juancavallotti/octo/runtime/blocks/controlflow"
)
