package odoo

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestCallRunsMethodOnRecords(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `true`)
	proc, err := newCall(types.Settings{
		"connector": "odoo",
		"model":     `"sale.order"`,
		"method":    `"action_confirm"`,
		"ids":       "body.orderId",
		"context":   `{"lang": "es_AR"}`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCall: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"orderId": 42}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rec.path != "/json/2/sale.order/action_confirm" {
		t.Errorf("path = %q", rec.path)
	}
	want := map[string]any{"ids": []any{float64(42)}, "context": map[string]any{"lang": "es_AR"}}
	if !reflect.DeepEqual(rec.body, want) {
		t.Errorf("body = %v, want %v", rec.body, want)
	}
	if out.Body != true {
		t.Errorf("message body = %v, want the method's result", out.Body)
	}
}

func TestCallRunsModelMethodIntoVariable(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `[[3, "Azure Interior"]]`)
	proc, err := newCall(types.Settings{
		"connector": "odoo",
		"model":     `"res.partner"`,
		"method":    `"name_search"`,
		"params":    `{"name": body.q, "limit": 5}`,
		"resultVar": "matches",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCall: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"q": "azure"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if _, ok := rec.body["ids"]; ok {
		t.Error("no ids configured, so the method runs on the model")
	}
	if rec.body["name"] != "azure" || rec.body["limit"] != float64(5) {
		t.Errorf("body = %v", rec.body)
	}
	if !reflect.DeepEqual(out.Variables["matches"], []any{[]any{float64(3), "Azure Interior"}}) {
		t.Errorf("matches = %v", out.Variables["matches"])
	}
	if !reflect.DeepEqual(out.Body, map[string]any{"q": "azure"}) {
		t.Errorf("body changed to %v; a result variable leaves it alone", out.Body)
	}
}

func TestCallFailOnError(t *testing.T) {
	srv, _ := stubServer(t, http.StatusForbidden, `{"name":"odoo.exceptions.AccessError","message":"no"}`)
	settings := types.Settings{"connector": "odoo", "model": `"res.partner"`, "method": `"unlink"`, "ids": "1"}

	proc, err := newCall(settings, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCall: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, "in")); err == nil {
		t.Error("expected the Odoo error to fail the flow by default")
	}

	settings["failOnError"] = false
	proc, err = newCall(settings, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCall: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, "in"))
	if err != nil || out.Body != "in" {
		t.Errorf("Process = %v, %v; want the message unchanged", out, err)
	}
}

func TestCallRequiresModelAndMethod(t *testing.T) {
	deps := blockDeps(t, "http://unused")
	for _, settings := range []types.Settings{
		{"connector": "odoo", "method": `"read"`},
		{"connector": "odoo", "model": `"res.partner"`},
	} {
		if _, err := newCall(settings, deps); err == nil {
			t.Errorf("newCall(%v): expected an error", settings)
		}
	}
}
