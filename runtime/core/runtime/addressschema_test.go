package runtime

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/schema"
)

// The resolver has no branch table of its own: it descends through a block by
// reading the block's schema, the same derivation the generated capabilities
// carry. This pins the one thing that can still drift — that every composite the
// runtime ships publishes branches, so an address can reach inside it — and that
// the resolver's lookup agrees with the generated catalogue block for block.
func TestSchemaAddressBranchesReachTheResolver(t *testing.T) {
	caps, err := schema.Generate(core.DefaultSchemaRegistry())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	composites := 0
	for _, block := range caps.Blocks {
		got, err := schema.Branches(core.DefaultSchemaRegistry(), block.Type)
		if err != nil {
			t.Fatalf("Branches(%q): %v", block.Type, err)
		}
		if block.AddressBranches == nil {
			if got != nil {
				t.Errorf("block %q: the resolver sees branches %v the catalogue does not publish", block.Type, got)
			}
			continue
		}
		composites++
		if got == nil {
			t.Errorf("block %q publishes branches the resolver cannot see", block.Type)
			continue
		}
		if !equalStrings(got.Named, block.AddressBranches.Named) ||
			!equalStrings(got.ByMember, block.AddressBranches.ByMember) {
			t.Errorf("block %q: resolver branches %v/%v, catalogue %v/%v", block.Type,
				got.Named, got.ByMember, block.AddressBranches.Named, block.AddressBranches.ByMember)
		}
	}

	if composites == 0 {
		t.Fatal("no composite published any address branches; the derivation is not running")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
