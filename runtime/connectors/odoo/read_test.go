package odoo

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestSearchReadSendsQuery(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `[{"id": 3, "name": "Ada"}]`)
	proc, err := newSearchRead(types.Settings{
		"connector": "odoo",
		"model":     `"res.partner"`,
		"domain":    `[["email", "=", body.email]]`,
		"fields":    []any{"name"},
		"limit":     5,
		"order":     "id desc",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearchRead: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"email": "ada@example.com"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rec.path != "/json/2/res.partner/search_read" {
		t.Errorf("path = %q", rec.path)
	}
	want := map[string]any{
		"domain": []any{[]any{"email", "=", "ada@example.com"}},
		"fields": []any{"name"},
		"limit":  float64(5),
		"order":  "id desc",
	}
	if !reflect.DeepEqual(rec.body, want) {
		t.Errorf("body = %v, want %v", rec.body, want)
	}
	if !reflect.DeepEqual(out.Body, []any{map[string]any{"id": float64(3), "name": "Ada"}}) {
		t.Errorf("message body = %v", out.Body)
	}
}

func TestSearchReadWithoutDomainMatchesEverything(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `[]`)
	proc, err := newSearchRead(types.Settings{"connector": "odoo", "model": `"res.partner"`}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearchRead: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !reflect.DeepEqual(rec.body, map[string]any{"domain": []any{}}) {
		t.Errorf("body = %v, want an empty domain only", rec.body)
	}
}

func TestSearchReadRejectsNonListDomain(t *testing.T) {
	srv, _ := stubServer(t, http.StatusOK, `[]`)
	proc, err := newSearchRead(types.Settings{
		"connector": "odoo", "model": `"res.partner"`, "domain": `{"email": "x"}`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSearchRead: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected an error for a domain that is not a list")
	}
}

func TestReadSendsIDsAndFields(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `[{"id": 3, "name": "Ada"}]`)
	proc, err := newRead(types.Settings{
		"connector": "odoo",
		"model":     `"res.partner"`,
		"ids":       "[3, 4]",
		"fields":    []any{"name"},
		"resultVar": "partners",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRead: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, nil))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := map[string]any{"ids": []any{float64(3), float64(4)}, "fields": []any{"name"}}
	if rec.path != "/json/2/res.partner/read" || !reflect.DeepEqual(rec.body, want) {
		t.Errorf("request = %s %v, want read %v", rec.path, rec.body, want)
	}
	if _, ok := out.Variables["partners"].([]any); !ok {
		t.Errorf("partners = %v", out.Variables["partners"])
	}
}

func TestReadRequiresIDs(t *testing.T) {
	if _, err := newRead(types.Settings{"connector": "odoo", "model": `"res.partner"`},
		blockDeps(t, "http://unused")); err == nil {
		t.Error("expected an error when ids is not set")
	}
}
