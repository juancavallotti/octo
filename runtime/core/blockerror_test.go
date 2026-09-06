package core

import (
	"errors"
	"fmt"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestFailingBlock(t *testing.T) {
	blockFailure := &BlockError{Label: "charge", Err: errors.New("card declined")}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a block failure names its block",
			err:  blockFailure,
			want: "charge",
		},
		{
			name: "wrapping does not hide the label",
			err:  fmt.Errorf("error path: %w", blockFailure),
			want: "charge",
		},
		{
			name: "an error from outside a block names nothing",
			err:  errors.New("connector never started"),
			want: "",
		},
		{
			name: "nested wrapping still unwraps",
			err:  fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", blockFailure)),
			want: "charge",
		},
		{
			name: "no error names nothing",
			err:  nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FailingBlock(tt.err); got != tt.want {
				t.Errorf("FailingBlock(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// SetErrorVariable reads the label through FailingBlock, so the two must agree:
// vars.error.block is the same string a metric would label a failure with.
func TestSetErrorVariableUsesTheSameLabel(t *testing.T) {
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	failure := fmt.Errorf("error path: %w", &BlockError{Label: "notify", Err: errors.New("boom")})

	SetErrorVariable(msg, "orders", failure)

	raw, ok := msg.Variables[ErrorVariable]
	if !ok {
		t.Fatal("vars.error not set")
	}
	e, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("vars.error is %T, want map[string]any", raw)
	}
	if e["block"] != FailingBlock(failure) {
		t.Errorf("vars.error.block = %v, want %q", e["block"], FailingBlock(failure))
	}
	if e["block"] != "notify" {
		t.Errorf("vars.error.block = %v, want %q", e["block"], "notify")
	}
	if e["flow"] != "orders" {
		t.Errorf("vars.error.flow = %v, want %q", e["flow"], "orders")
	}
}
