package odoo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func startConnector(t *testing.T, set map[string]any) *Connector {
	t.Helper()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: set}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

func TestConnectorRegistered(t *testing.T) {
	if _, err := core.DefaultRegistry().New(connectorType); err != nil {
		t.Fatalf("connector %q not registered: %v", connectorType, err)
	}
}

func TestStartValidatesSettings(t *testing.T) {
	cases := map[string]map[string]any{
		"missing url":     {"apiKey": "k"},
		"missing api key": {"url": "https://odoo.example.com"},
		"unknown version": {"url": "https://odoo.example.com", "apiKey": "k", "version": "12"},
		"version 18 without database": {
			"url": "https://odoo.example.com", "apiKey": "k", "version": "18", "username": "admin",
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Connector{}
			if err := c.Start(context.Background(), types.ConnectorConfig{Settings: set}); err == nil {
				t.Error("expected Start to fail")
			}
		})
	}
}

func TestStartAcceptsNumericVersion(t *testing.T) {
	conn := startConnector(t, map[string]any{
		"version": 18, "url": "https://odoo.example.com", "apiKey": "k", "database": "prod", "username": "admin",
	})
	if _, ok := conn.client.(*jsonrpcClient); !ok {
		t.Errorf("client = %T, want the JSON-RPC client for version 18", conn.client)
	}
}

// recorded is what a stub server saw of one request.
type recorded struct {
	path    string
	headers http.Header
	body    map[string]any
}

// stubServer answers every request with status and response, recording it.
func stubServer(t *testing.T, status int, response string) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.headers = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		rec.body = nil
		_ = json.Unmarshal(raw, &rec.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestJSON2CallsRecordMethod(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `true`)
	conn := startConnector(t, map[string]any{"url": srv.URL + "/", "apiKey": "secret", "database": "prod"})

	result, err := conn.Call(context.Background(), Request{
		Model:   "res.partner",
		Method:  "write",
		IDs:     []int64{7},
		Params:  map[string]any{"vals": map[string]any{"phone": "123"}},
		Context: map[string]any{"lang": "es_AR"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result != true {
		t.Errorf("result = %v, want true", result)
	}
	if rec.path != "/json/2/res.partner/write" {
		t.Errorf("path = %q", rec.path)
	}
	if got := rec.headers.Get("Authorization"); got != "bearer secret" {
		t.Errorf("Authorization = %q", got)
	}
	if got := rec.headers.Get("X-Odoo-Database"); got != "prod" {
		t.Errorf("X-Odoo-Database = %q", got)
	}
	want := map[string]any{
		"ids":     []any{float64(7)},
		"vals":    map[string]any{"phone": "123"},
		"context": map[string]any{"lang": "es_AR"},
	}
	if !reflect.DeepEqual(rec.body, want) {
		t.Errorf("body = %v, want %v", rec.body, want)
	}
}

func TestJSON2ModelMethodSendsNoIDs(t *testing.T) {
	srv, rec := stubServer(t, http.StatusOK, `[{"id":1}]`)
	conn := startConnector(t, map[string]any{"url": srv.URL, "apiKey": "secret"})

	if _, err := conn.Call(context.Background(), Request{
		Model: "res.partner", Method: "search_read", Params: map[string]any{"limit": 1},
	}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if _, ok := rec.body["ids"]; ok {
		t.Error("a model method must not send ids")
	}
	if rec.headers.Get("X-Odoo-Database") != "" {
		t.Error("no database configured, so no X-Odoo-Database header")
	}
}

func TestJSON2ReportsServerError(t *testing.T) {
	srv, _ := stubServer(t, http.StatusUnprocessableEntity,
		`{"name":"odoo.exceptions.ValidationError","message":"Contacts require a name","debug":"Traceback..."}`)
	conn := startConnector(t, map[string]any{"url": srv.URL, "apiKey": "secret"})

	_, err := conn.Call(context.Background(), Request{Model: "res.partner", Method: "create"})
	var oe *Error
	if !errors.As(err, &oe) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if oe.Name != "odoo.exceptions.ValidationError" || oe.Message != "Contacts require a name" {
		t.Errorf("error = %+v", oe)
	}
}

func TestJSON2ReportsNonJSONFailure(t *testing.T) {
	srv, _ := stubServer(t, http.StatusBadGateway, `<html>bad gateway</html>`)
	conn := startConnector(t, map[string]any{"url": srv.URL, "apiKey": "secret"})

	_, err := conn.Call(context.Background(), Request{Model: "res.partner", Method: "read", IDs: []int64{}})
	var oe *Error
	if !errors.As(err, &oe) || oe.Message != "HTTP 502: <html>bad gateway</html>" {
		t.Fatalf("err = %v", err)
	}
}
