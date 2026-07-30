// Package pinecone provides the "pinecone" connector: a configured Pinecone
// index that upsert/query/fetch/delete blocks bind to by name. The connector
// owns the API key and index addressing; namespace is addressed per block call
// (optionally per message, via CEL), falling back to the connector's default.
//
// Start looks the index up by name, which does two things at once: it resolves
// the index's data-plane host, and it validates eagerly — comparing the index's
// actual dimension against the configured one, so a mismatched embedding model
// and index (the classic Pinecone failure mode) fails at startup rather than on
// the first upsert, mirroring how the database connector pings its DSN eagerly.
//
// Configure host — the index host the Pinecone console shows — and the lookup is
// skipped: startup then touches the network not at all, which is what a flow test
// or an air-gapped build needs. The dimension goes unchecked in that case, so the
// trade is stated plainly rather than made silently.
package pinecone

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync/atomic"

	sdk "github.com/pinecone-io/go-pinecone/v5/pinecone"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerQuery()
	registerUpsert()
	registerFetch()
	registerDelete()
}

func registerConnector() {
	core.MustRegisterConnector("pinecone", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the pinecone connector and every
	// pinecone-* block share the Pinecone palette group and brand icon unless
	// they set their own — mirrors the notion package.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: displayName})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "pinecone",
		Label:    displayName,
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// displayName is the editor-facing label, palette group, and icon for the
// pinecone connector and its blocks.
const displayName = "Pinecone"

// connectorSettings are the knobs the pinecone connector exposes.
type connectorSettings struct {
	// Authenticates with the Pinecone API; source from ${PINECONE_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Name of the Pinecone index to connect to.
	Index string `json:"index" octo:"label=Index name,required"`
	// The index's host, as shown in the Pinecone console. Optional: given one, the
	// connector addresses the index directly and skips the startup lookup (and with
	// it the dimension check).
	Host string `json:"host" octo:"label=Index host"`
	// Expected vector dimension. Checked against the index's actual dimension at
	// startup — when the connector looks the index up, i.e. when host is unset — so
	// a mismatch fails fast rather than on the first upsert.
	Dimension int `json:"dimension" octo:"label=Dimension"`
	// Default namespace used when a block doesn't specify its own.
	Namespace string `json:"namespace" octo:"label=Default namespace"`
}

// Connector is a configured Pinecone index that upsert/query/fetch/delete
// blocks bind to by name. It is safe for concurrent use: the SDK client and
// index connection are, and the connector holds only immutable state after
// Start.
//
// The index connection is atomic because it is the one field that is not
// immutable after Start: Stop takes it away, and a block whose message is still
// in flight reads it at the same moment. An ordinary pointer would be a data
// race whose failure mode is a nil dereference during shutdown.
type Connector struct {
	client  *sdk.Client
	idxConn atomic.Pointer[sdk.IndexConnection]
}

var _ core.Connector = (*Connector)(nil)

// Start decodes and validates the settings, builds the Pinecone client,
// resolves the index's host — looking the index up and checking its dimension
// unless a host was configured — and opens the data-plane connection, so a bad
// API key, a missing index, or a dimension mismatch all fail at startup rather
// than on first use.
func (c *Connector) Start(ctx context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return fmt.Errorf("pinecone connector %q: apiKey is required", config.Name)
	}
	if strings.TrimSpace(set.Index) == "" {
		return fmt.Errorf("pinecone connector %q: index is required", config.Name)
	}

	client, err := sdk.NewClient(sdk.NewClientParams{ApiKey: set.APIKey})
	if err != nil {
		return fmt.Errorf("pinecone connector %q: %w", config.Name, err)
	}

	host, err := resolveHost(ctx, client, set)
	if err != nil {
		return fmt.Errorf("pinecone connector %q: %w", config.Name, err)
	}

	idxConn, err := client.Index(sdk.NewIndexConnParams{Host: host, Namespace: set.Namespace})
	if err != nil {
		return fmt.Errorf("pinecone connector %q: connect to index %q: %w", config.Name, set.Index, err)
	}

	c.client = client
	c.idxConn.Store(idxConn)
	slog.Info("pinecone connector started",
		"connector", config.Name, "index", set.Index,
		"dimension", set.Dimension, "dimensionChecked", set.Host == "")
	return nil
}

// resolveHost returns the index's data-plane host: the configured one when it is
// set, otherwise the one the control plane reports — which is also where the
// dimension gets checked, since describing the index is what makes the actual
// dimension knowable.
func resolveHost(ctx context.Context, client *sdk.Client, set connectorSettings) (string, error) {
	if set.Host != "" {
		return set.Host, nil
	}
	idx, err := client.DescribeIndex(ctx, set.Index)
	if err != nil {
		return "", fmt.Errorf("describe index %q: %w", set.Index, err)
	}
	if err := checkDimension(idx, set.Dimension); err != nil {
		return "", err
	}
	return idx.Host, nil
}

// checkDimension compares an index's actual dimension against the configured
// one, and is a no-op when none was configured — there is nothing to disagree
// with. Split out from Start so the check is testable without a live index.
func checkDimension(idx *sdk.Index, configured int) error {
	if configured <= 0 {
		return nil
	}
	if idx.Dimension == nil {
		return fmt.Errorf("index %q has no dimension (sparse indexes are not supported)", idx.Name)
	}
	if int(*idx.Dimension) != configured {
		return fmt.Errorf("index %q has dimension %d, configured dimension is %d", idx.Name, *idx.Dimension, configured)
	}
	return nil
}

// Stop closes the data-plane connection opened by Start. It takes the
// connection before closing it, so a second Stop is a no-op rather than a
// double close, and a block that reads the connection concurrently either gets
// the live one or the "not started" error — never a closed pointer half-way
// through being cleared.
func (c *Connector) Stop(context.Context) error {
	conn := c.idxConn.Swap(nil)
	if conn == nil {
		return nil
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("close pinecone index connection: %w", err)
	}
	return nil
}

// IndexConnection returns the data-plane connection scoped to namespace, or
// the connector's default namespace when namespace is empty. Blocks call this
// per message so each one can address a different namespace — Pinecone's
// multi-tenancy mechanism — while sharing the underlying gRPC connection:
// IndexConnection.WithNamespace is documented as cheap for exactly this reason,
// so there is no need to cache a connection per namespace.
//
// It errors rather than returning nil when the connector is not started (or is
// stopping), mirroring the database connector's DB(): a message still in flight
// during shutdown fails with a sentence that says what happened, instead of
// panicking on a nil connection.
func (c *Connector) IndexConnection(namespace string) (*sdk.IndexConnection, error) {
	conn := c.idxConn.Load()
	if conn == nil {
		return nil, fmt.Errorf("pinecone connector not started")
	}
	if namespace == "" {
		return conn, nil
	}
	return conn.WithNamespace(namespace), nil
}
