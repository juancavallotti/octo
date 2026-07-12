package schema

import (
	"fmt"
	"strings"
)

// The field types that carry a sub-flow an address can descend into. A block's
// branches are exactly these fields, so the octo tags that already declare a
// composite's slots are the one source of truth — the schema does not repeat a
// hand-maintained table that could drift from the resolver's.
//
// namedBranchTypes hold one chain, addressed by the field's own name. The other
// slot types (rule-list, skill-list, mcp-resource-list, …) hold data, not
// sub-flows, and are not addressable.
var namedBranchTypes = map[FieldType]bool{
	TypeFlow: true, "block-list": true,
}

// memberBranchTypes hold a list of sub-flows, each addressed by its own name (or
// its index when it has none) rather than by the slot's name.
var memberBranchTypes = map[FieldType]bool{
	"flow-list": true, "case-list": true, "route-list": true, "tool-list": true,
}

// addressBranchesOf derives the branches a block exposes to an address from its
// fields, returning nil for a leaf block (one with no sub-flow slots at all).
func addressBranchesOf(blockType string, fields []Field) *AddressBranches {
	var named, byMember []string
	for _, f := range fields {
		switch {
		case namedBranchTypes[f.Type]:
			named = append(named, f.Name)
		case memberBranchTypes[f.Type]:
			byMember = append(byMember, f.Name)
		}
	}
	if len(named) == 0 && len(byMember) == 0 {
		return nil
	}
	return &AddressBranches{
		Named:    named,
		ByMember: byMember,
		Note:     branchNote(blockType, named, byMember),
	}
}

// branchNote spells out how to address into this block, with a concrete example
// built from its own branches — the fact an agent needs in order to write a
// working address the first time.
func branchNote(blockType string, named, byMember []string) string {
	parts := []string{fmt.Sprintf(
		"A block on the way to an address target must name a branch of this one: %s[<branch>].<block>.",
		blockType)}

	if len(named) > 0 {
		parts = append(parts, fmt.Sprintf("Branches: %s — e.g. %s[%s].<block>.",
			strings.Join(named, ", "), blockType, named[0]))
	}
	if len(byMember) > 0 {
		parts = append(parts, fmt.Sprintf(
			"Each entry of %s is a branch, addressed by its own name (or its index when it has none) — "+
				"e.g. %s[<name>].<block> or %s[0].<block>.",
			strings.Join(byMember, ", "), blockType, blockType))
	}
	return strings.Join(parts, " ")
}
