package httpclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// restDeps starts an http-client connector pointed at baseURL and returns
// BlockDeps that resolve it under the name "api".
func restDeps(t *testing.T, baseURL string) core.BlockDeps {
	t.Helper()
	conn := startConnector(t, types.Settings{"baseURL": baseURL})
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "api" {
			return conn, true
		}
		return nil, false
	}}
}

func restMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

func TestRESTGetFoldsJSONAndSetsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("city"); got != "berlin" {
			t.Errorf("query city = %q, want berlin", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"temp": 21.5}`)
	}))
	defer srv.Close()

	proc, err := newREST(types.Settings{
		"connector": "api",
		"method":    "GET",
		"path":      "/weather",
		"query":     map[string]any{"city": "body.city"},
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}

	out, err := proc.Process(context.Background(), restMessage(t, map[string]any{"city": "berlin"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	obj, ok := out.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object body, got %T", out.Body)
	}
	if obj["temp"] != 21.5 {
		t.Errorf("temp = %v, want 21.5", obj["temp"])
	}
	if got := out.Variables["statusCode"]; got != 200 {
		t.Errorf("statusCode var = %v, want 200", got)
	}
}

func TestRESTPostSendsBodyAndHeaders(t *testing.T) {
	var gotBody map[string]any
	var gotContentType, gotCustom string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotCustom = r.Header.Get("X-Tenant")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"created": true}`)
	}))
	defer srv.Close()

	proc, err := newREST(types.Settings{
		"connector": "api",
		"method":    "POST",
		"path":      "/orders",
		"headers":   map[string]any{"X-Tenant": "vars.tenant"},
		"body":      "body",
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}

	msg := restMessage(t, map[string]any{"item": "widget"})
	msg.Variables.Set("tenant", "acme")
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotCustom != "acme" {
		t.Errorf("X-Tenant = %q, want acme", gotCustom)
	}
	if gotBody["item"] != "widget" {
		t.Errorf("server received body item = %v, want widget", gotBody["item"])
	}
	if got := out.Variables["statusCode"]; got != 201 {
		t.Errorf("statusCode var = %v, want 201", got)
	}
}

func TestRESTFailOnErrorDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"boom"}`)
	}))
	defer srv.Close()
	deps := restDeps(t, srv.URL)

	// Default: a 500 fails the message.
	failing, err := newREST(types.Settings{"connector": "api", "path": "/x"}, deps)
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}
	if _, err := failing.Process(context.Background(), restMessage(t, nil)); err == nil {
		t.Error("expected an error for a 500 response by default")
	}

	// failOnError: false keeps the message and records the status.
	tolerant, err := newREST(types.Settings{"connector": "api", "path": "/x", "failOnError": false}, deps)
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}
	out, err := tolerant.Process(context.Background(), restMessage(t, nil))
	if err != nil {
		t.Fatalf("Process with failOnError=false: %v", err)
	}
	if got := out.Variables["statusCode"]; got != 500 {
		t.Errorf("statusCode var = %v, want 500", got)
	}
}

func TestRESTRequiresConnector(t *testing.T) {
	deps := core.BlockDeps{Connector: func(string) (core.Connector, bool) { return nil, false }}
	if _, err := newREST(types.Settings{"path": "/x"}, deps); err == nil {
		t.Error("expected an error when connector is missing")
	}
	if _, err := newREST(types.Settings{"connector": "missing", "path": "/x"}, deps); err == nil {
		t.Error("expected an error for an unknown connector reference")
	}
}
