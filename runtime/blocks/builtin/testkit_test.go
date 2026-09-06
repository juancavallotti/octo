package builtin

import (
	"github.com/juancavallotti/octo/runtime/internal/testkit"
)

// The shared fixtures, under the names this package's tests grew up with.
var (
	withFakeServices = testkit.WithFakeServices
	mustMessage      = testkit.Message
)
