package schema

import (
	"reflect"
	"strings"
	"testing"
)

func TestAddressBranchesNamedSlots(t *testing.T) {
	fields := []Field{
		{Name: "process", Type: "block-list"},
		{Name: "error", Type: "block-list"},
	}

	got := addressBranchesOf("handle-errors", fields)
	if got == nil {
		t.Fatal("addressBranchesOf = nil, want the composite's branches")
	}
	if want := []string{"process", "error"}; !reflect.DeepEqual(got.Named, want) {
		t.Errorf("named = %v, want %v", got.Named, want)
	}
	if len(got.ByMember) != 0 {
		t.Errorf("byMember = %v, want none", got.ByMember)
	}
	// The note must carry a usable example, since that is the whole point: an agent
	// should be able to write a correct address without a failed attempt first.
	if !strings.Contains(got.Note, "handle-errors[process].<block>") {
		t.Errorf("note = %q, want a concrete address example", got.Note)
	}
}

func TestAddressBranchesMemberSlots(t *testing.T) {
	fields := []Field{
		{Name: "cases", Type: "case-list"},
		{Name: "default", Type: TypeFlow},
	}

	got := addressBranchesOf("switch", fields)
	if got == nil {
		t.Fatal("addressBranchesOf = nil, want the composite's branches")
	}
	if want := []string{"default"}; !reflect.DeepEqual(got.Named, want) {
		t.Errorf("named = %v, want %v", got.Named, want)
	}
	if want := []string{"cases"}; !reflect.DeepEqual(got.ByMember, want) {
		t.Errorf("byMember = %v, want %v", got.ByMember, want)
	}
}

// A leaf has nothing to descend into, and slots that hold data rather than
// sub-flows (rules, skills, a plain CEL expression) are not branches.
func TestAddressBranchesOmittedForLeaf(t *testing.T) {
	fields := []Field{
		{Name: "message", Type: "cel"},
		{Name: "rules", Type: "rule-list"},
		{Name: "skills", Type: "skill-list"},
		{Name: "level", Type: TypeString},
	}

	if got := addressBranchesOf("log", fields); got != nil {
		t.Errorf("addressBranchesOf = %+v, want nil for a block with no sub-flow slots", got)
	}
}
