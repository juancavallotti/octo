package deployment

import (
	"context"
	"slices"

	"github.com/juancavallotti/octo/orchestrator/internal/runtimeversion"
)

// Runtimes is what a deployment can be pinned to: the release the install
// deploys, and every release a deployment runs now, newest first. Only releases
// are listed, since only a release can be pinned.
func (s *Service) Runtimes(ctx context.Context) (current string, versions []string, err error) {
	current = s.CurrentRuntime()
	deps, err := s.List(ctx)
	if err != nil {
		return "", nil, err
	}
	seen := map[string]bool{}
	add := func(v string) {
		if runtimeversion.IsRelease(v) && !seen[v] {
			seen[v] = true
			versions = append(versions, v)
		}
	}
	add(current)
	for _, d := range deps {
		_, v := d.Runtime()
		add(v)
	}
	slices.SortFunc(versions, func(a, b string) int {
		switch {
		case runtimeversion.Behind(a, b):
			return 1
		case runtimeversion.Behind(b, a):
			return -1
		}
		return 0
	})
	return current, versions, nil
}
