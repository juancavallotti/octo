package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/juancavallotti/octo/types"
)

// startSQLite starts a database connector backed by a fresh temp-file SQLite
// database and registers cleanup. It returns the started connector.
func startSQLite(t *testing.T) *Connector {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db")
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name: "test-db",
		Type: "database",
		Settings: types.Settings{
			"driver": "sqlite",
			"dsn":    dsn,
		},
	}
	if err := c.Start(context.Background(), cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

func TestStartOpensAndPings(t *testing.T) {
	c := startSQLite(t)

	db, err := c.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), "CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatalf("Exec: %v", err)
	}
}

func TestStartRejectsUnknownDriver(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name:     "test-db",
		Settings: types.Settings{"driver": "oracle", "dsn": "whatever"},
	}
	if err := c.Start(context.Background(), cfg); err == nil {
		t.Fatal("expected an error for an unknown driver")
	}
}

func TestStartRequiresDSN(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name:     "test-db",
		Settings: types.Settings{"driver": "sqlite"},
	}
	if err := c.Start(context.Background(), cfg); err == nil {
		t.Fatal("expected an error when dsn is missing")
	}
}

func TestStartRejectsBadLifetime(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name: "test-db",
		Settings: types.Settings{
			"driver":          "sqlite",
			"dsn":             "file:" + filepath.Join(t.TempDir(), "t.db"),
			"connMaxLifetime": "forever",
		},
	}
	if err := c.Start(context.Background(), cfg); err == nil {
		t.Fatal("expected an error for an unparseable connMaxLifetime")
	}
}

func TestStopClosesPool(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{
		Name: "test-db",
		Settings: types.Settings{
			"driver": "sqlite",
			"dsn":    "file:" + filepath.Join(t.TempDir(), "t.db"),
		},
	}
	if err := c.Start(context.Background(), cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := c.DB(); err == nil {
		t.Fatal("expected DB() to error after Stop")
	}
}
