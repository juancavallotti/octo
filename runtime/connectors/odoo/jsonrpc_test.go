package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// rpcCall is one JSON-RPC request a stub server received.
type rpcCall struct {
	Service string `json:"service"`
	Method  string `json:"method"`
	Args    []any  `json:"args"`
}

// legacyServer answers authenticate with uid and every execute_kw with
// result, recording the execute_kw calls and counting the authentications.
func legacyServer(t *testing.T, uid any, result string) (*httptest.Server, *[]rpcCall, *atomic.Int32) {
	t.Helper()
	var calls []rpcCall
	var auths atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jsonrpc" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var env struct {
			Params rpcCall `json:"params"`
		}
		_ = json.Unmarshal(raw, &env)
		w.Header().Set("Content-Type", "application/json")
		if env.Params.Service == "common" && env.Params.Method == "authenticate" {
			auths.Add(1)
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "result": uid})
			_, _ = w.Write(out)
			return
		}
		calls = append(calls, env.Params)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":` + result + `}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &auths
}

func legacySettings(url string) map[string]any {
	return map[string]any{
		"version": "18", "url": url, "apiKey": "secret", "database": "prod", "username": "admin",
	}
}

func TestStartVersion18RequiresDatabaseAndUsername(t *testing.T) {
	c := &Connector{}
	set := map[string]any{"version": "18", "url": "https://odoo.example.com", "apiKey": "k", "database": "prod"}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: set}); err == nil {
		t.Error("expected Start to fail without a username")
	}
}

func TestJSONRPCLaysOutExecuteKw(t *testing.T) {
	srv, calls, auths := legacyServer(t, 2, `true`)
	conn := startConnector(t, legacySettings(srv.URL))

	for range 2 {
		if _, err := conn.Call(context.Background(), Request{
			Model:   "res.partner",
			Method:  "write",
			IDs:     []int64{7},
			Params:  map[string]any{"vals": map[string]any{"phone": "123"}},
			Context: map[string]any{"lang": "es_AR"},
		}); err != nil {
			t.Fatalf("Call: %v", err)
		}
	}
	if got := auths.Load(); got != 1 {
		t.Errorf("authenticated %d times, want once", got)
	}
	want := rpcCall{Service: "object", Method: "execute_kw", Args: []any{
		"prod", float64(2), "secret", "res.partner", "write",
		[]any{[]any{float64(7)}},
		map[string]any{"vals": map[string]any{"phone": "123"}, "context": map[string]any{"lang": "es_AR"}},
	}}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Errorf("call = %#v\nwant   %#v", (*calls)[0], want)
	}
}

func TestJSONRPCPassesCreateValuesPositionally(t *testing.T) {
	srv, calls, _ := legacyServer(t, 2, `[10]`)
	conn := startConnector(t, legacySettings(srv.URL))

	result, err := conn.Call(context.Background(), Request{
		Model: "res.partner", Method: "create",
		Params: map[string]any{"vals_list": []any{map[string]any{"name": "Ada"}}},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !reflect.DeepEqual(result, []any{float64(10)}) {
		t.Errorf("result = %v", result)
	}
	args := (*calls)[0].Args
	if !reflect.DeepEqual(args[5], []any{[]any{map[string]any{"name": "Ada"}}}) {
		t.Errorf("args = %v, want vals_list as the only positional argument", args[5])
	}
	if !reflect.DeepEqual(args[6], map[string]any{}) {
		t.Errorf("kwargs = %v, want empty", args[6])
	}
}

func TestJSONRPCRejectedLoginIsAccessDenied(t *testing.T) {
	srv, calls, auths := legacyServer(t, false, `true`)
	conn := startConnector(t, legacySettings(srv.URL))

	for range 2 {
		_, err := conn.Call(context.Background(), Request{Model: "res.partner", Method: "search_read"})
		var oe *Error
		if !errors.As(err, &oe) || oe.Name != "odoo.exceptions.AccessDenied" {
			t.Fatalf("err = %v, want AccessDenied", err)
		}
	}
	if len(*calls) != 0 {
		t.Error("no execute_kw should be sent without a user id")
	}
	if got := auths.Load(); got != 2 {
		t.Errorf("authenticated %d times, want a retry per call after a rejection", got)
	}
}

func TestJSONRPCReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":200,"message":"Odoo Server Error",` +
			`"data":{"name":"odoo.exceptions.ValidationError","message":"Contacts require a name","debug":"..."}}}`))
	}))
	t.Cleanup(srv.Close)
	conn := startConnector(t, legacySettings(srv.URL))

	_, err := conn.Call(context.Background(), Request{Model: "res.partner", Method: "create"})
	var oe *Error
	if !errors.As(err, &oe) {
		t.Fatalf("err = %v, want *Error", err)
	}
	if oe.Name != "odoo.exceptions.ValidationError" || oe.Message != "Contacts require a name" {
		t.Errorf("error = %+v", oe)
	}
}

func TestJSONRPCReportsErrorStatusWithoutErrorMember(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if bytes.Contains(raw, []byte(`"authenticate"`)) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":2}`))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"maintenance"}`))
	}))
	t.Cleanup(srv.Close)
	conn := startConnector(t, legacySettings(srv.URL))

	_, err := conn.Call(context.Background(), Request{Model: "res.partner", Method: "search_read"})
	var oe *Error
	if !errors.As(err, &oe) || oe.Message != `HTTP 503: {"status":"maintenance"}` {
		t.Fatalf("err = %v, want an HTTP 503 error", err)
	}
}
