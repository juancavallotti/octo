package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// newDeps starts a database connector backed by a fresh temp-file SQLite database
// seeded with an orders table, and returns BlockDeps that resolve it under the
// name "orders-db".
func newDeps(t *testing.T) core.BlockDeps {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "orders.db")
	conn := &Connector{}
	cfg := types.ConnectorConfig{
		Name:     "orders-db",
		Type:     "database",
		Settings: types.Settings{"driver": "sqlite", "dsn": dsn},
	}
	if err := conn.Start(context.Background(), cfg); err != nil {
		t.Fatalf("connector Start: %v", err)
	}
	t.Cleanup(func() { _ = conn.Stop(context.Background()) })

	db, err := conn.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE orders (id INTEGER PRIMARY KEY AUTOINCREMENT, item TEXT NOT NULL, amount REAL NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

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

func TestSQLInsertReturningSingle(t *testing.T) {
	deps := newDeps(t)
	proc, err := newSQL(types.Settings{
		"connector": "orders-db",
		"query":     "INSERT INTO orders (item, amount) VALUES (?, ?) RETURNING *",
		"args":      []any{"body.item", "body.amount"},
		"single":    true,
	}, deps)
	if err != nil {
		t.Fatalf("newSQL: %v", err)
	}

	msg := newMessage(t, map[string]any{"item": "widget", "amount": 1500})
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	row, ok := out.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected a single row object, got %T", out.Body)
	}
	if row["item"] != "widget" {
		t.Errorf("item = %v, want widget", row["item"])
	}
	if row["amount"] != float64(1500) {
		t.Errorf("amount = %v (%T), want float64 1500", row["amount"], row["amount"])
	}
	if row["id"] != float64(1) {
		t.Errorf("id = %v (%T), want float64 1", row["id"], row["id"])
	}
}

func TestSQLSelectSingleByVar(t *testing.T) {
	deps := newDeps(t)
	insert, err := newSQL(types.Settings{
		"connector": "orders-db",
		"query":     "INSERT INTO orders (item, amount) VALUES (?, ?)",
		"args":      []any{"body.item", "body.amount"},
		"exec":      true,
	}, deps)
	if err != nil {
		t.Fatalf("newSQL insert: %v", err)
	}
	insertMsg := newMessage(t, map[string]any{"item": "gadget", "amount": 12.5})
	execOut, err := insert.Process(context.Background(), insertMsg)
	if err != nil {
		t.Fatalf("insert Process: %v", err)
	}
	if affected := execOut.Body.(map[string]any)["rowsAffected"]; affected != float64(1) {
		t.Errorf("rowsAffected = %v, want 1", affected)
	}

	sel, err := newSQL(types.Settings{
		"connector": "orders-db",
		"query":     "SELECT * FROM orders WHERE id = ?",
		"args":      []any{"vars.id"},
		"single":    true,
	}, deps)
	if err != nil {
		t.Fatalf("newSQL select: %v", err)
	}
	selMsg := newMessage(t, nil)
	selMsg.Variables.Set("id", "1")
	out, err := sel.Process(context.Background(), selMsg)
	if err != nil {
		t.Fatalf("select Process: %v", err)
	}
	row, ok := out.Body.(map[string]any)
	if !ok {
		t.Fatalf("expected a row object, got %T", out.Body)
	}
	if row["item"] != "gadget" {
		t.Errorf("item = %v, want gadget", row["item"])
	}
}

func TestSQLSelectSingleNoRowsIsNull(t *testing.T) {
	deps := newDeps(t)
	proc, err := newSQL(types.Settings{
		"connector": "orders-db",
		"query":     "SELECT * FROM orders WHERE id = ?",
		"args":      []any{"vars.id"},
		"single":    true,
	}, deps)
	if err != nil {
		t.Fatalf("newSQL: %v", err)
	}
	msg := newMessage(t, nil)
	msg.Variables.Set("id", "999")
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.Body != nil {
		t.Errorf("expected null body for no rows, got %v", out.Body)
	}
}

func TestSQLQueryReturnsArray(t *testing.T) {
	deps := newDeps(t)
	insert, err := newSQL(types.Settings{
		"connector": "orders-db",
		"query":     "INSERT INTO orders (item, amount) VALUES ('a', 1), ('b', 2)",
	}, deps)
	if err != nil {
		t.Fatalf("newSQL insert: %v", err)
	}
	if _, err := insert.Process(context.Background(), newMessage(t, nil)); err != nil {
		t.Fatalf("insert: %v", err)
	}

	sel, err := newSQL(types.Settings{
		"connector": "orders-db",
		"query":     "SELECT item FROM orders ORDER BY id",
	}, deps)
	if err != nil {
		t.Fatalf("newSQL select: %v", err)
	}
	out, err := sel.Process(context.Background(), newMessage(t, nil))
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	rows, ok := out.Body.([]any)
	if !ok {
		t.Fatalf("expected an array body, got %T", out.Body)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
}

func TestSQLUnknownConnectorErrors(t *testing.T) {
	deps := core.BlockDeps{Connector: func(string) (core.Connector, bool) { return nil, false }}
	if _, err := newSQL(types.Settings{"connector": "missing", "query": "SELECT 1"}, deps); err == nil {
		t.Fatal("expected an error for an unknown connector reference")
	}
}

func TestSQLRequiresQueryAndConnector(t *testing.T) {
	deps := newDeps(t)
	if _, err := newSQL(types.Settings{"connector": "orders-db"}, deps); err == nil {
		t.Error("expected an error when query is missing")
	}
	if _, err := newSQL(types.Settings{"query": "SELECT 1"}, deps); err == nil {
		t.Error("expected an error when connector is missing")
	}
}
