package core_test

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
)

// TestNewMocksValidatesSpecs: a mock is validated at the edge, so the flow builder
// can trust what it is handed and a mistake in the spec is reported against the flag
// the user typed rather than surfacing as a strange failure mid-flow.
func TestNewMocksValidatesSpecs(t *testing.T) {
	tests := []struct {
		name string
		spec core.MockSpec
		want string // empty means the spec is valid
	}{
		{
			name: "a case with a body",
			spec: core.MockSpec{Cases: []core.MockCase{{When: "true", Body: map[string]any{"ok": true}}}},
		},
		{
			name: "a case with a body and vars",
			spec: core.MockSpec{Cases: []core.MockCase{
				{When: "true", Body: map[string]any{"ok": true}, Vars: map[string]any{"status": 200}},
			}},
		},
		{
			name: "a case that fails the block",
			spec: core.MockSpec{Cases: []core.MockCase{{When: "true", Error: "card declined"}}},
		},
		{
			name: "a case that drops the message",
			spec: core.MockSpec{Cases: []core.MockCase{{When: "true", Drop: true}}},
		},
		{
			name: "a default on its own",
			spec: core.MockSpec{Default: &core.MockCase{Body: map[string]any{}}},
		},
		{
			name: "a false body is still a body",
			spec: core.MockSpec{Cases: []core.MockCase{{When: "true", Body: false}}},
		},
		{
			name: "no cases and no default",
			spec: core.MockSpec{},
			want: "at least one case, or a default",
		},
		{
			name: "a case with no condition",
			spec: core.MockSpec{Cases: []core.MockCase{{Body: map[string]any{}}}},
			want: "needs a `when`",
		},
		{
			name: "a case with no outcome",
			spec: core.MockSpec{Cases: []core.MockCase{{When: "true"}}},
			want: "needs an outcome",
		},
		{
			name: "a case with two outcomes",
			spec: core.MockSpec{Cases: []core.MockCase{{When: "true", Body: map[string]any{}, Drop: true}}},
			want: "more than one outcome",
		},
		{
			name: "vars without a body",
			spec: core.MockSpec{Cases: []core.MockCase{
				{When: "true", Error: "nope", Vars: map[string]any{"status": 500}},
			}},
			want: "`vars` only goes with a `body`",
		},
		{
			name: "a default with a condition",
			spec: core.MockSpec{Default: &core.MockCase{When: "true", Body: map[string]any{}}},
			want: "the default must not have a `when`",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := core.NewMocks(map[string]core.MockSpec{"orders.charge": tc.spec})

			if tc.want == "" {
				if err != nil {
					t.Fatalf("NewMocks rejected a valid spec: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("NewMocks accepted an invalid spec, want an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "orders.charge") {
				t.Errorf("error = %q, want it to name the address at fault", err)
			}
		})
	}
}

func TestNewMocksRejectsAnEmptyAddress(t *testing.T) {
	_, err := core.NewMocks(map[string]core.MockSpec{
		"": {Cases: []core.MockCase{{When: "true", Drop: true}}},
	})
	if err == nil {
		t.Fatal("an empty address must be rejected")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %q, want it to mention the empty address", err)
	}
}

// TestMocksAddressesAreSorted: injection walks these in order and rewrites the config
// as it goes, so the order has to be stable — otherwise two runs of the same config
// could build different trees.
func TestMocksAddressesAreSorted(t *testing.T) {
	spec := core.MockSpec{Cases: []core.MockCase{{When: "true", Drop: true}}}
	mocks, err := core.NewMocks(map[string]core.MockSpec{
		"orders.charge": spec,
		"orders.audit":  spec,
		"other.step":    spec,
	})
	if err != nil {
		t.Fatalf("NewMocks: %v", err)
	}

	want := []string{"orders.audit", "orders.charge", "other.step"}
	got := mocks.Addresses()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Addresses() = %v, want %v", got, want)
		}
	}
	if _, ok := mocks.For("orders.charge"); !ok {
		t.Error("For(orders.charge) found nothing")
	}
	if _, ok := mocks.For("orders.nope"); ok {
		t.Error("For(orders.nope) found a spec that was never given")
	}
}
