// Package database provides a connector that owns a database/sql connection pool.
// A sql block binds to it by name and runs statements through its DB(); the
// connector opens the pool on Start and closes it on Stop. One connector type
// serves both flavors, selected by the "driver" setting: "postgres" (jackc/pgx)
// or "sqlite" (modernc.org/sqlite). Both are pure Go, so no CGO toolchain is
// needed.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	// Pure-Go drivers, registered for use via database/sql. Both are always
	// linked so either flavor can be selected at runtime by the "driver" setting.
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	_ "modernc.org/sqlite"             // registers the "sqlite" driver

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterConnector("database", func() core.Connector {
		return &Connector{}
	})
}

// driverNames maps the connector's "driver" setting to the database/sql driver
// name registered by the imported driver packages.
var driverNames = map[string]string{
	"postgres": "pgx",
	"sqlite":   "sqlite",
}

// connectorSettings are the knobs the database connector exposes. Driver and DSN
// are required; the pool tuning fields are optional and left at database/sql
// defaults when zero.
type connectorSettings struct {
	// Driver selects the flavor: "postgres" or "sqlite".
	Driver string `json:"driver"`
	// DSN is the data source name passed to the driver: a Postgres connection
	// string, or a SQLite file path / "file:..." URI.
	DSN string `json:"dsn"`
	// MaxOpenConns caps the open connection pool (0 = unlimited, the default).
	MaxOpenConns int `json:"maxOpenConns"`
	// MaxIdleConns caps idle connections kept in the pool.
	MaxIdleConns int `json:"maxIdleConns"`
	// ConnMaxLifetime is a duration string (e.g. "5m") bounding connection reuse.
	ConnMaxLifetime string `json:"connMaxLifetime"`
}

// Connector is a configured database connection pool that flows' sql blocks run
// statements through. The pool is opened on Start and closed on Stop; a *sql.DB
// is safe for concurrent use, matching the shared-connector contract.
type Connector struct {
	db *sql.DB
}

// Start parses the settings, opens the pool, applies tuning, and verifies the
// connection so a bad DSN fails fast at startup rather than on first query.
func (c *Connector) Start(ctx context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if set.DSN == "" {
		return fmt.Errorf("database connector %q: dsn is required", config.Name)
	}

	driverName, ok := driverNames[set.Driver]
	if !ok {
		return fmt.Errorf("database connector %q: driver %q is not one of postgres/sqlite", config.Name, set.Driver)
	}

	db, err := sql.Open(driverName, set.DSN)
	if err != nil {
		return fmt.Errorf("database connector %q: open: %w", config.Name, err)
	}

	if set.MaxOpenConns > 0 {
		db.SetMaxOpenConns(set.MaxOpenConns)
	}
	if set.MaxIdleConns > 0 {
		db.SetMaxIdleConns(set.MaxIdleConns)
	}
	if set.ConnMaxLifetime != "" {
		lifetime, parseErr := time.ParseDuration(set.ConnMaxLifetime)
		if parseErr != nil {
			_ = db.Close()
			return fmt.Errorf("database connector %q: connMaxLifetime %q: %w", config.Name, set.ConnMaxLifetime, parseErr)
		}
		db.SetConnMaxLifetime(lifetime)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("database connector %q: ping: %w", config.Name, err)
	}

	c.db = db
	slog.Info("database connector started", "connector", config.Name, "driver", set.Driver)
	return nil
}

// Stop closes the connection pool if one was opened.
func (c *Connector) Stop(context.Context) error {
	if c.db == nil {
		return nil
	}
	err := c.db.Close()
	c.db = nil
	if err != nil {
		return fmt.Errorf("close database pool: %w", err)
	}
	return nil
}

// DB returns the connection pool. It is the capability a sql block binds to by
// referencing this connector by name.
func (c *Connector) DB() (*sql.DB, error) {
	if c.db == nil {
		return nil, fmt.Errorf("database connector not started")
	}
	return c.db, nil
}
