package mongodb

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// depsWith builds the BlockDeps a block factory sees, with one mongodb
// connector registered under "orders-db".
func depsWith(conn core.Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "orders-db" {
			return conn, true
		}
		return nil, false
	}}
}

func newMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

func TestResolveConnector(t *testing.T) {
	conn := &Connector{}
	tests := []struct {
		name      string
		connector string
		deps      core.BlockDeps
		wantErr   bool
	}{
		{name: "found", connector: "orders-db", deps: depsWith(conn)},
		{name: "empty name", connector: "", deps: depsWith(conn), wantErr: true},
		{name: "not configured", connector: "other-db", deps: depsWith(conn), wantErr: true},
		{name: "no connectors at all", connector: "orders-db", deps: core.BlockDeps{}, wantErr: true},
		{
			// A name that resolves to some other connector type is a config
			// mistake worth naming, not a panic on a bad type assertion.
			name:      "wrong connector type",
			connector: "orders-db",
			deps: core.BlockDeps{Connector: func(string) (core.Connector, bool) {
				return notMongo{}, true
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveConnector(tt.connector, tt.deps)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveConnector: %v", err)
			}
			if got != conn {
				t.Errorf("resolveConnector returned %v, want the registered connector", got)
			}
		})
	}
}

func TestNewTargetValidation(t *testing.T) {
	tests := []struct {
		name       string
		database   string
		collection string
	}{
		{name: "collection is required", collection: ""},
		{name: "collection must compile", collection: "body."},
		{name: "database must compile", database: "body.", collection: `"orders"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newTarget(nil, "mongodb-find", tt.database, tt.collection); err == nil {
				t.Error("expected a build-time error")
			}
		})
	}
}

// The address is per message so one flow can serve several tenants, which only
// works if both halves really evaluate against the message.
func TestTargetResolvesPerMessage(t *testing.T) {
	tgt, err := newTarget(nil, "mongodb-find", "body.tenant", `"orders"`)
	if err != nil {
		t.Fatalf("newTarget: %v", err)
	}
	conn := &Connector{}
	msg := newMessage(t, map[string]any{"tenant": "acme"})

	// Not started, so this fails at the connector rather than the address —
	// which is the point: the expressions evaluated, and the failure names the
	// real reason.
	_, err = tgt.resolve(conn, expr.MessageActivation(msg, nil))
	if err == nil {
		t.Fatal("expected an error from a connector that was never started")
	}
	if got := err.Error(); got != "mongodb connector not started" {
		t.Errorf("err = %q, want the not-started error", got)
	}
}

// A filter is an expression, so it has to be able to reach body and vars —
// otherwise it can only ever match constants.
func TestEvalDocumentReadsTheMessage(t *testing.T) {
	program, err := expr.CompileMessage(nil, `{"sku": body.sku, "tenant": vars.tenant}`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	msg := newMessage(t, map[string]any{"sku": "A1"})
	msg.Variables.Set("tenant", "acme")

	doc, err := evalDocument("filter", program, expr.MessageActivation(msg, nil))
	if err != nil {
		t.Fatalf("evalDocument: %v", err)
	}
	if got := doc.Lookup("sku").StringValue(); got != "A1" {
		t.Errorf("sku = %q, want %q", got, "A1")
	}
	if got := doc.Lookup("tenant").StringValue(); got != "acme" {
		t.Errorf("tenant = %q, want %q", got, "acme")
	}
}

func TestEvalDocumentRejectsNonObjects(t *testing.T) {
	program, err := expr.CompileMessage(nil, `body.sku`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	msg := newMessage(t, map[string]any{"sku": "A1"})
	if _, err := evalDocument("filter", program, expr.MessageActivation(msg, nil)); err == nil {
		t.Error("expected an error for an expression that is not an object")
	}
}

// An absent optional field yields a nil document, which the driver reads as
// "no filter" / "no projection" — so unset means unset, not an empty object
// the caller has to special-case.
func TestEvalOptionalDocumentUnset(t *testing.T) {
	doc, err := evalOptionalDocument("projection", nil, nil)
	if err != nil {
		t.Fatalf("evalOptionalDocument: %v", err)
	}
	if doc != nil {
		t.Errorf("doc = %v, want nil", doc)
	}
}

// Body by default, variable when named — and both paths carry the same
// decoded-JSON kinds, because both go through JSON.
func TestDeliver(t *testing.T) {
	t.Run("body", func(t *testing.T) {
		msg := newMessage(t, map[string]any{"keep": "me"})
		if err := deliver(msg, "", map[string]any{"deletedCount": 3}); err != nil {
			t.Fatalf("deliver: %v", err)
		}
		body, ok := msg.Body.(map[string]any)
		if !ok {
			t.Fatalf("body is %T, want map[string]any", msg.Body)
		}
		// float64, not int: the message's numbers are decoded-JSON numbers, and a
		// CEL comparison against an int would need a literal nobody writes.
		if body["deletedCount"] != float64(3) {
			t.Errorf("deletedCount = %#v, want float64 3", body["deletedCount"])
		}
	})

	t.Run("variable leaves the body alone", func(t *testing.T) {
		msg := newMessage(t, map[string]any{"keep": "me"})
		if err := deliver(msg, "result", map[string]any{"deletedCount": 3}); err != nil {
			t.Fatalf("deliver: %v", err)
		}
		body, ok := msg.Body.(map[string]any)
		if !ok || body["keep"] != "me" {
			t.Errorf("body = %#v, want the incoming body untouched", msg.Body)
		}
		value, ok := msg.Variables["result"].(map[string]any)
		if !ok {
			t.Fatalf("vars.result is %T, want map[string]any", msg.Variables["result"])
		}
		if value["deletedCount"] != float64(3) {
			t.Errorf("vars.result.deletedCount = %#v, want float64 3", value["deletedCount"])
		}
	})
}

func TestDeliverJSON(t *testing.T) {
	raw := []byte(`[{"sku":"A1"}]`)

	t.Run("body", func(t *testing.T) {
		msg := newMessage(t, nil)
		if err := deliverJSON(msg, "", raw); err != nil {
			t.Fatalf("deliverJSON: %v", err)
		}
		docs, ok := msg.Body.([]any)
		if !ok || len(docs) != 1 {
			t.Fatalf("body = %#v, want a one-element list", msg.Body)
		}
	})

	t.Run("variable", func(t *testing.T) {
		msg := newMessage(t, "unchanged")
		if err := deliverJSON(msg, "docs", raw); err != nil {
			t.Fatalf("deliverJSON: %v", err)
		}
		if msg.Body != "unchanged" {
			t.Errorf("body = %#v, want it left alone", msg.Body)
		}
		if _, ok := msg.Variables["docs"].([]any); !ok {
			t.Errorf("vars.docs is %T, want []any", msg.Variables["docs"])
		}
	})
}

// notMongo stands in for a connector of some other type registered under a name
// a mongodb block asked for.
type notMongo struct{}

func (notMongo) Start(context.Context, types.ConnectorConfig) error { return nil }
func (notMongo) Stop(context.Context) error                         { return nil }
