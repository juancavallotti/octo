package deployment

import (
	"github.com/juancavallotti/octo/orchestrator/internal/runtimeversion"
)

// Runtimes is what a deployment can be pinned to: the release the install
// deploys and the supported releases no newer than it, newest first.
func (s *Service) Runtimes() (current string, versions []string) {
	current = s.CurrentRuntime()
	return current, runtimeversion.Pinnable(current)
}
