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
	"reflect"
	"time"

	// Pure-Go drivers, registered for use via database/sql. Both are always
	// linked so either flavor can be selected at runtime by the "driver" setting.
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	_ "modernc.org/sqlite"             // registers the "sqlite" driver

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterConnector("database", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the database connector and its sql block fall
	// into the Integration palette group with the Database icon unless they set
	// their own.
	core.RegisterExtension(core.ExtensionMeta{Group: "Integration", Icon: "Database"})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "database",
		Label:    "Database",
		Settings: reflect.TypeFor[connectorSettings](),
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
	// Database driver.
	Driver string `json:"driver" octo:"label=Driver,type=enum,enum=postgres|sqlite,required"`
	// Data source name (Postgres connection string or SQLite file path).
	DSN string `json:"dsn" octo:"label=DSN,required"`
	// Password merged into the DSN at startup, so only it has to be a secret and the
	// DSN can stay in plain config. Postgres only.
	Password string `json:"password" octo:"label=Password"`
	// Open connection pool cap (0 = unlimited).
	MaxOpenConns int `json:"maxOpenConns" octo:"label=Max open connections"`
	// Idle connections kept in the pool.
	MaxIdleConns int `json:"maxIdleConns" octo:"label=Max idle connections"`
	// Duration bounding connection reuse (e.g. 5m).
	ConnMaxLifetime string `json:"connMaxLifetime" octo:"label=Connection max lifetime"`
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

	dsn, err := withPassword(set.Driver, set.DSN, set.Password, config.Name)
	if err != nil {
		return fmt.Errorf("database connector %q: %w", config.Name, err)
	}

	db, err := sql.Open(driverName, dsn)
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
