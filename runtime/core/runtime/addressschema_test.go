package runtime

import (
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/schema"
)

// listSlots are the block-config slots holding a list of sub-flows, each addressed
// by its member's own name or index. They mirror what listMembers flattens; the
// schema names the slot, the resolver walks it, and this is where the two meet.
var listSlots = map[string]bool{
	"branches": true, "cases": true, "routes": true, "tools": true,
}

// The schema's addressBranches and the resolver's branch table are two derivations
// of the same fact — which chains a composite exposes to an address. The schema
// reads the octo slot tags; the resolver reads the block config. This pins them
// together, so renaming a slot on one side cannot silently leave the other (and the
// agents reading the schema) addressing a branch that no longer resolves.
func TestSchemaAddressBranchesMatchResolver(t *testing.T) {
	caps, err := schema.Generate(core.DefaultSchemaRegistry())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	claimed := make(map[string]bool)
	composites := 0
	for _, block := range caps.Blocks {
		if block.AddressBranches == nil {
			continue
		}
		composites++
		for _, name := range block.AddressBranches.Named {
			if _, ok := reservedBranches[name]; !ok {
				t.Errorf("block %q publishes branch %q, which the resolver cannot descend into",
					block.Type, name)
			}
			claimed[name] = true
		}
		for _, slot := range block.AddressBranches.ByMember {
			if !listSlots[slot] {
				t.Errorf("block %q publishes member-addressed slot %q, which listMembers does not flatten",
					block.Type, slot)
			}
		}
	}

	if composites == 0 {
		t.Fatal("no composite published any address branches; the derivation is not running")
	}
	// Every branch the resolver accepts must be reachable from the schema, or an
	// agent reading the schema would never learn it exists.
	for name := range reservedBranches {
		if !claimed[name] {
			t.Errorf("resolver accepts branch %q, but no block publishes it in its schema", name)
		}
	}
}
