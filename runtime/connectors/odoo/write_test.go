package odoo

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestCreateWrapsOneRecordInAList(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `[12]`)
	proc, err := newCreate(types.Settings{
		"connector": "odoo",
		"model":     `"crm.lead"`,
		"values":    `{"name": body.subject, "email_from": body.email}`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCreate: %v", err)
	}
	out, err := proc.Process(context.Background(),
		blockMessage(t, map[string]any{"subject": "Demo", "email": "ada@example.com"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := map[string]any{"vals_list": []any{map[string]any{"name": "Demo", "email_from": "ada@example.com"}}}
	if rec.path != "/json/2/crm.lead/create" || !reflect.DeepEqual(rec.body, want) {
		t.Errorf("request = %s %v, want create %v", rec.path, rec.body, want)
	}
	if !reflect.DeepEqual(out.Body, []any{float64(12)}) {
		t.Errorf("message body = %v, want the new ids", out.Body)
	}
}

func TestCreateSendsListAsIs(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `[12, 13]`)
	proc, err := newCreate(types.Settings{
		"connector": "odoo", "model": `"res.partner"`, "values": `[{"name": "A"}, {"name": "B"}]`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCreate: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := rec.body["vals_list"].([]any); len(got) != 2 {
		t.Errorf("vals_list = %v, want both records", got)
	}
}

func TestCreateRejectsScalarValues(t *testing.T) {
	srv, _ := stubServer(t, http.StatusOK, `[]`)
	proc, err := newCreate(types.Settings{"connector": "odoo", "model": `"res.partner"`, "values": `"Ada"`},
		blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newCreate: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected an error for values that are not an object")
	}
}

func TestWriteSendsIDsAndVals(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `true`)
	proc, err := newWrite(types.Settings{
		"connector": "odoo", "model": `"crm.lead"`, "ids": "body.id", "values": `{"priority": "2"}`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newWrite: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"id": 12})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := map[string]any{"ids": []any{float64(12)}, "vals": map[string]any{"priority": "2"}}
	if rec.path != "/json/2/crm.lead/write" || !reflect.DeepEqual(rec.body, want) {
		t.Errorf("request = %s %v, want write %v", rec.path, rec.body, want)
	}
}

func TestDeleteCallsUnlink(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `true`)
	proc, err := newDelete(types.Settings{"connector": "odoo", "model": `"crm.lead"`, "ids": "[12, 13]"},
		blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newDelete: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	want := map[string]any{"ids": []any{float64(12), float64(13)}}
	if rec.path != "/json/2/crm.lead/unlink" || !reflect.DeepEqual(rec.body, want) {
		t.Errorf("request = %s %v, want unlink %v", rec.path, rec.body, want)
	}
}

func TestWriteBlocksRequireIDs(t *testing.T) {
	deps := blockDeps(t, "http://unused")
	if _, err := newWrite(types.Settings{"connector": "odoo", "model": `"crm.lead"`, "values": "{}"}, deps); err == nil {
		t.Error("odoo-write: expected an error when ids is not set")
	}
	if _, err := newDelete(types.Settings{"connector": "odoo", "model": `"crm.lead"`}, deps); err == nil {
		t.Error("odoo-delete: expected an error when ids is not set")
	}
}
