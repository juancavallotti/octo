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

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerSQL()
}

func registerConnector() {
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
	"postgres":   "pgx",
	driverSQLite: driverSQLite,
}

// Pool defaults applied when the corresponding setting is left unset (zero for
// the ints, empty for the lifetime). database/sql's own defaults — unlimited
// open, 2 idle, connections reused forever — are wrong for a pool a flow's
// workers share: 2 idle throttles anything beyond trivial concurrency into a
// connect-per-query storm.
//
// Postgres and SQLite get different defaults because they have different
// concurrency stories: Postgres benefits from a real pool sized against a
// shared server's connection budget, while SQLite's single-writer semantics
// make concurrent connections against one file a source of "database is
// locked" contention rather than a throughput win.
const (
	// Sized independently of any flow's worker count — this connector has no
	// visibility into which flows bind to it — but comfortably above the
	// pre-#197 default of 8 workers, and low enough that several replicas of
	// the runtime each opening this connector stay well under a typical
	// Postgres max_connections of ~100.
	defaultPostgresMaxOpenConns = 16
	// Equal to MaxOpenConns so the pool doesn't open and immediately close
	// connections under bursty load.
	defaultPostgresMaxIdleConns = 16
	// Long enough not to needlessly recycle healthy connections, short enough
	// that a connection sitting behind a proxy or load balancer (pgbouncer, a
	// cloud SQL proxy, a Service during a rolling deploy) gets rebalanced
	// within a reasonable window instead of pinned forever.
	defaultPostgresConnMaxLifetime = 30 * time.Minute

	// Capped at one connection: concurrent writers against a single SQLite
	// file contend rather than parallelize. A caller running WAL mode and
	// wanting more concurrency can still set maxOpenConns explicitly.
	defaultSQLiteMaxOpenConns = 1
	defaultSQLiteMaxIdleConns = 1
	// Unlimited: a local file has no proxy or load balancer to rebalance
	// behind, so there is nothing forcing periodic reconnection to gain.
	defaultSQLiteConnMaxLifetime = 0
)

// connectorSettings are the knobs the database connector exposes. Driver and DSN
// are required; the pool tuning fields are optional and default per driver (see
// resolvePoolSettings) when zero.
type connectorSettings struct {
	// Database driver.
	Driver string `json:"driver" octo:"label=Driver,type=enum,enum=postgres|sqlite,required"`
	// Data source name (Postgres connection string or SQLite file path).
	DSN string `json:"dsn" octo:"label=DSN,required"`
	// Password merged into the DSN at startup, so only it has to be a secret and the
	// DSN can stay in plain config. Postgres only.
	Password string `json:"password" octo:"label=Password"`
	// Open connection pool cap (0 = driver default; see resolvePoolSettings).
	MaxOpenConns int `json:"maxOpenConns" octo:"label=Max open connections"`
	// Idle connections kept in the pool (0 = driver default).
	MaxIdleConns int `json:"maxIdleConns" octo:"label=Max idle connections"`
	// Duration bounding connection reuse (e.g. 5m; "" = driver default).
	ConnMaxLifetime string `json:"connMaxLifetime" octo:"label=Connection max lifetime"`
}

// resolvePoolSettings fills in whichever pool setting was left unset with the
// driver-appropriate default, and parses ConnMaxLifetime. It is separate from
// Start so the defaulting logic is testable without opening a real database.
func resolvePoolSettings(set connectorSettings) (maxOpen, maxIdle int, lifetime time.Duration, err error) {
	defaultMaxOpen, defaultMaxIdle := defaultPostgresMaxOpenConns, defaultPostgresMaxIdleConns
	defaultLifetime := defaultPostgresConnMaxLifetime
	if set.Driver == driverSQLite {
		defaultMaxOpen, defaultMaxIdle = defaultSQLiteMaxOpenConns, defaultSQLiteMaxIdleConns
		defaultLifetime = defaultSQLiteConnMaxLifetime
	}

	maxOpen = orDefaultInt(set.MaxOpenConns, defaultMaxOpen)
	maxIdle = orDefaultInt(set.MaxIdleConns, defaultMaxIdle)

	lifetime = defaultLifetime
	if set.ConnMaxLifetime != "" {
		lifetime, err = time.ParseDuration(set.ConnMaxLifetime)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("parse connMaxLifetime: %w", err)
		}
		// time.ParseDuration accepts a signed duration, but SetConnMaxLifetime
		// treats <= 0 as unlimited — a negative value would silently bypass the
		// driver's default instead of erroring or unambiguously meaning unlimited.
		if lifetime < 0 {
			return 0, 0, 0, fmt.Errorf("connMaxLifetime %q must not be negative", set.ConnMaxLifetime)
		}
	}
	return maxOpen, maxIdle, lifetime, nil
}

// orDefaultInt returns v when it is positive, else def.
func orDefaultInt(v, def int) int {
	if v > 0 {
		return v
	}
	return def
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

	maxOpen, maxIdle, lifetime, err := resolvePoolSettings(set)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("database connector %q: %w", config.Name, err)
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	if lifetime > 0 {
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
