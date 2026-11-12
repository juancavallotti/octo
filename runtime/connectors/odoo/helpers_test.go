package odoo

import (
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/core/expr"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// blockDeps starts a JSON-2 connector against baseURL and resolves it under
// the name "odoo".
func blockDeps(t *testing.T, baseURL string) core.BlockDeps {
	t.Helper()
	conn := startConnector(t, map[string]any{"url": baseURL, "apiKey": "secret"})
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "odoo" {
			return conn, true
		}
		return nil, false
	}}
}

func blockMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

func TestToIDAcceptsNumbersAndMany2one(t *testing.T) {
	for _, value := range []any{float64(7), []any{float64(7), "Azure Interior"}} {
		if id, err := toID(value); err != nil || id != 7 {
			t.Errorf("toID(%v) = %d, %v; want 7", value, id, err)
		}
	}
	for _, value := range []any{float64(1.5), float64(0), "7", nil} {
		if _, err := toID(value); err == nil {
			t.Errorf("toID(%v): expected an error", value)
		}
	}
}

func TestEvalIDsShapes(t *testing.T) {
	cases := map[string][]int64{
		"7":                          {7},
		"[7, 8]":                     {7, 8},
		"[]":                         {},
		`[7, "Azure Interior"]`:      {7},
		`[[7, "Azure Interior"], 8]`: {7, 8},
	}
	for src, want := range cases {
		program, err := expr.CompileMessage(nil, src)
		if err != nil {
			t.Fatalf("compile %s: %v", src, err)
		}
		got, err := evalIDs(program, map[string]any{})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("evalIDs(%s) = %v, %v; want %v", src, got, err, want)
		}
	}
}
