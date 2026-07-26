package core_test

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

func TestBlockLabel(t *testing.T) {
	tests := []struct {
		label     string
		name      string
		blockType string
		ref       string
		want      string
	}{
		{label: "name wins", name: "charge", blockType: "rest", ref: "charge-api", want: "charge"},
		{label: "type when unnamed", blockType: "rest", ref: "charge-api", want: "rest"},
		{label: "ref when only ref", ref: "charge-api", want: "charge-api"},
		{label: "empty when nothing is set", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			if got := core.BlockLabel(tt.name, tt.blockType, tt.ref); got != tt.want {
				t.Fatalf("BlockLabel(%q, %q, %q) = %q, want %q", tt.name, tt.blockType, tt.ref, got, tt.want)
			}
		})
	}
}

func TestMemberBranch(t *testing.T) {
	if got := core.MemberBranch("audit", 2); got != "audit" {
		t.Fatalf("named member = %q, want %q", got, "audit")
	}
	if got := core.MemberBranch("", 2); got != "2" {
		t.Fatalf("unnamed member = %q, want %q", got, "2")
	}
}

func TestJoin(t *testing.T) {
	// The grammar, spelled out end to end: orders.checkHeader[else].api-call-1.
	path := core.JoinBlock(core.JoinBranch(core.JoinBlock("orders", "checkHeader"), "else"), "api-call-1")
	if want := "orders.checkHeader[else].api-call-1"; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if got := core.JoinBranch("orders", "error"); got != "orders[error]" {
		t.Fatalf("error chain = %q, want %q", got, "orders[error]")
	}
}
