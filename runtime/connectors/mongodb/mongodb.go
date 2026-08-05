// Package mongodb provides a connector that owns a MongoDB client, and the
// blocks that read and write documents through it. The connector opens the
// client on Start and pings the deployment, so a bad URI or an unreachable
// server fails at startup rather than on the first query, and disconnects on
// Stop. A block binds to it by name and addresses a database and collection per
// message.
//
// Documents cross the message boundary as MongoDB Extended JSON v2 — relaxed by
// default, canonical when the extendedJson setting asks for it. That is what
// makes the connector symmetric: the document a find returns can be handed back
// to a filter unchanged, ObjectId and dates included, which plain JSON cannot
// express.
//
// go.mongodb.org/mongo-driver is pure Go, so no CGO toolchain is needed.
package mongodb

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerFind()
	registerInsert()
	registerUpdate()
	registerDelete()
	registerAggregate()
}

func registerConnector() {
	core.MustRegisterConnector(connectorType, func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the mongodb connector and every mongodb-*
	// block share the MongoDB palette group and brand icon unless they set their
	// own — mirrors the pinecone package.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: displayName})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     connectorType,
		Label:    displayName,
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

const (
	// connectorType is the type a flow names in its connectors block, and the
	// prefix every block of this package registers under.
	connectorType = "mongodb"
	// displayName is the editor-facing label, palette group, and icon for the
	// mongodb connector and its blocks.
	displayName = "MongoDB"
	// appName is reported to the server, so a deployment's currentOp and logs
	// attribute connections to Octo rather than to an anonymous driver.
	appName = "octo"
)

// Extended JSON modes accepted by the extendedJson setting.
const (
	extendedJSONRelaxed   = "relaxed"
	extendedJSONCanonical = "canonical"
)

// Pool and timeout defaults, applied when the corresponding setting is unset
// and the connection string does not carry its own. The driver's own
// maxPoolSize default is 100, a number chosen with no knowledge of how much
// concurrency will actually arrive; Octo does have that number, so the default
// here is stated in terms of it.
const (
	// Sized to the worker count a flow gets by default — max(8, GOMAXPROCS*4),
	// i.e. 32 on an eight-core host (see resolveWorkers in core/runtime) — so
	// every worker of one flow can hold a checked-out connection instead of
	// queueing on the pool. Like the database connector, this one cannot see
	// which flows bind to it, so this is a justified constant rather than a
	// derivation: a deployment pointing many flows at one connector should raise
	// it, and one running dozens of replicas against a small server should lower
	// it.
	defaultMaxPoolSize = 32
	// A client-level CSOT bounding every operation, server selection included,
	// so a wedged query cannot pin a worker forever. Matches the http-client
	// connector's default timeout.
	defaultTimeout = 30 * time.Second
)

// connectorSettings are the knobs the mongodb connector exposes. Only uri is
// required; the pool and timeout fields default per resolveClientOptions.
type connectorSettings struct {
	// Connection string, with no password in it: mongodb://user@host:27017 or
	// mongodb+srv://user@cluster.example.net.
	URI string `json:"uri" octo:"label=URI,required"`
	// Password merged into the URI at startup, so only it has to be a secret and
	// the URI can stay in plain config; source it from ${MONGODB_PASSWORD}.
	Password string `json:"password" octo:"label=Password"`
	// Database blocks address when they do not name one of their own.
	Database string `json:"database" octo:"label=Default database"`
	// How BSON types JSON cannot express — ObjectId, dates, Decimal128, 64-bit
	// integers — are rendered into and read back from a message. Relaxed leaves
	// ordinary values as plain JSON; canonical types every value, losing nothing
	// but turning every number into an object.
	ExtendedJSON string `json:"extendedJson" octo:"label=Extended JSON,type=enum,enum=relaxed|canonical,default=relaxed"`
	// Connections the pool may open (0 = 32; see defaultMaxPoolSize).
	MaxPoolSize int `json:"maxPoolSize" octo:"label=Max pool size"`
	// Connections kept open while idle (0 = the driver default, which is none).
	MinPoolSize int `json:"minPoolSize" octo:"label=Min pool size"`
	// How long an idle connection is kept before it is closed (e.g. 5m;
	// "" = the driver default, which is forever).
	MaxConnIdleTime string `json:"maxConnIdleTime" octo:"label=Max connection idle time"`
	// Ceiling on a single operation, server selection included (e.g. 10s;
	// "" = 30s).
	Timeout string `json:"timeout" octo:"label=Operation timeout"`
}

// Connector is a configured MongoDB deployment that the mongodb-* blocks bind
// to by name. It is safe for concurrent use: the driver's Client is, and the
// connector holds only immutable state after Start.
//
// The client is atomic because it is the one field that is not immutable after
// Start: Stop takes it away, and a block whose message is still in flight reads
// it at the same moment. An ordinary pointer would be a data race whose failure
// mode is a nil dereference during shutdown.
type Connector struct {
	client atomic.Pointer[mongo.Client]
	// database and canonical are written once by Start, before any block can
	// run, and only read afterwards.
	database  string
	canonical bool
}

var _ core.Connector = (*Connector)(nil)

// Start decodes and validates the settings, merges the password into the URI,
// opens the client, and pings the deployment — so a malformed URI, a wrong
// credential, or an unreachable server all fail at startup rather than on the
// first message.
func (c *Connector) Start(ctx context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.URI) == "" {
		return fmt.Errorf("mongodb connector %q: uri is required", config.Name)
	}

	canonical, err := resolveExtendedJSON(set.ExtendedJSON)
	if err != nil {
		return fmt.Errorf("mongodb connector %q: %w", config.Name, err)
	}
	uri, err := withPassword(set.URI, set.Password, config.Name)
	if err != nil {
		return fmt.Errorf("mongodb connector %q: %w", config.Name, err)
	}
	opts, err := resolveClientOptions(uri, set)
	if err != nil {
		return fmt.Errorf("mongodb connector %q: %w", config.Name, err)
	}

	client, err := mongo.Connect(opts)
	if err != nil {
		return fmt.Errorf("mongodb connector %q: connect: %w", config.Name, err)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		_ = client.Disconnect(ctx)
		return fmt.Errorf("mongodb connector %q: ping: %w", config.Name, err)
	}

	c.database = set.Database
	c.canonical = canonical
	c.client.Store(client)
	slog.Info("mongodb connector started",
		"connector", config.Name, "database", set.Database, "canonicalExtendedJSON", canonical)
	return nil
}

// Stop disconnects the client opened by Start. It takes the client before
// closing it, so a second Stop is a no-op rather than a double disconnect, and
// a block that reads the client concurrently either gets the live one or the
// "not started" error — never a closed one half-way through being cleared.
func (c *Connector) Stop(ctx context.Context) error {
	client := c.client.Swap(nil)
	if client == nil {
		return nil
	}
	if err := client.Disconnect(ctx); err != nil {
		return fmt.Errorf("disconnect mongodb client: %w", err)
	}
	return nil
}

// Collection returns a handle on database.collection, falling back to the
// connector's default database when database is empty. Blocks call it per
// message so one flow can address a different database or collection per
// tenant, the way the pinecone connector scopes a connection to a namespace.
//
// It errors rather than returning nil when the connector is not started (or is
// stopping), mirroring the database connector's DB(): a message still in flight
// during shutdown fails with a sentence that says what happened, instead of
// panicking on a nil client.
func (c *Connector) Collection(database, collection string) (*mongo.Collection, error) {
	client := c.client.Load()
	if client == nil {
		return nil, fmt.Errorf("mongodb connector not started")
	}
	if database == "" {
		database = c.database
	}
	if database == "" {
		return nil, fmt.Errorf("no database: name one on the block, or set a default on the connector")
	}
	if collection == "" {
		return nil, fmt.Errorf("collection is required")
	}
	return client.Database(database).Collection(collection), nil
}

// Canonical reports whether this connector's blocks render documents as
// canonical Extended JSON rather than relaxed.
func (c *Connector) Canonical() bool {
	return c.canonical
}

// resolveExtendedJSON maps the extendedJson setting onto the canonical flag the
// bson marshaler takes. An unrecognized mode is a startup error rather than a
// silent fallback: the two modes produce visibly different bodies, so guessing
// would leave a flow quietly reading the shape it did not ask for.
func resolveExtendedJSON(mode string) (bool, error) {
	switch strings.TrimSpace(mode) {
	case "", extendedJSONRelaxed:
		return false, nil
	case extendedJSONCanonical:
		return true, nil
	default:
		return false, fmt.Errorf("extendedJson %q is not one of %s/%s",
			mode, extendedJSONRelaxed, extendedJSONCanonical)
	}
}

// resolveClientOptions turns the settings into driver options. It is separate
// from Start so the defaulting is testable without a running deployment.
//
// A configured setting wins over the same option spelled in the connection
// string, matching how the password setting overrides the URI's. A default only
// fills a gap neither of them filled — so a URI carrying ?maxPoolSize=50 keeps
// its 50 instead of being quietly overwritten by this package's 32.
func resolveClientOptions(uri string, set connectorSettings) (*options.ClientOptions, error) {
	maxPool, err := poolSize("maxPoolSize", set.MaxPoolSize)
	if err != nil {
		return nil, err
	}
	minPool, err := poolSize("minPoolSize", set.MinPoolSize)
	if err != nil {
		return nil, err
	}
	idle, err := parseSetDuration("maxConnIdleTime", set.MaxConnIdleTime)
	if err != nil {
		return nil, err
	}
	timeout, err := parseSetDuration("timeout", set.Timeout)
	if err != nil {
		return nil, err
	}

	opts := options.Client().ApplyURI(uri).SetAppName(appName)
	if maxPool > 0 {
		opts.SetMaxPoolSize(maxPool)
	} else if opts.MaxPoolSize == nil {
		opts.SetMaxPoolSize(defaultMaxPoolSize)
	}
	if minPool > 0 {
		opts.SetMinPoolSize(minPool)
	}
	if idle > 0 {
		opts.SetMaxConnIdleTime(idle)
	}
	if timeout > 0 {
		opts.SetTimeout(timeout)
	} else if opts.Timeout == nil {
		opts.SetTimeout(defaultTimeout)
	}
	return opts, nil
}

// poolSize validates a pool bound and widens it to the uint64 the driver takes.
// A negative bound is a config error rather than a huge unsigned one.
func poolSize(field string, value int) (uint64, error) {
	if value < 0 {
		return 0, fmt.Errorf("%s must not be negative", field)
	}
	return uint64(value), nil
}

// parseSetDuration parses a configured duration, treating empty as unset (0) so
// the caller can tell "leave it alone" from a real value. A negative duration is
// an error because the driver reads it as "no limit", which would silently
// bypass the default the author was trying to change.
func parseSetDuration(field, value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must not be negative", field)
	}
	return parsed, nil
}
