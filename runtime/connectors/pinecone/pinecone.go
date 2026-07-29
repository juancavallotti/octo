// Package pinecone provides the "pinecone" connector: a configured Pinecone
// index that upsert/query/fetch/delete blocks bind to by name. The connector
// owns the API key and index addressing; namespace is addressed per block call
// (optionally per message, via CEL), falling back to the connector's default.
//
// Start validates eagerly: it describes the index and compares its actual
// dimension against the configured one, so a mismatched embedding model and
// index — the classic Pinecone failure mode — fails at startup rather than on
// the first upsert, mirroring how the database connector pings its DSN eagerly.
package pinecone

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	sdk "github.com/pinecone-io/go-pinecone/v5/pinecone"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
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
	// Expected vector dimension; compared against the index's actual dimension at
	// startup so a mismatch fails fast rather than on the first upsert.
	Dimension int `json:"dimension" octo:"label=Dimension,required"`
	// Default namespace used when a block doesn't specify its own.
	Namespace string `json:"namespace" octo:"label=Default namespace"`
	// Overrides the Pinecone control-plane API endpoint (for testing).
	Host string `json:"host" octo:"label=API host"`
}

// Connector is a configured Pinecone index that upsert/query/fetch/delete
// blocks bind to by name. It is safe for concurrent use: the SDK client and
// index connection are, and the connector holds only immutable state after
// Start.
type Connector struct {
	client  *sdk.Client
	idxConn *sdk.IndexConnection
}

var _ core.Connector = (*Connector)(nil)

// Start decodes and validates the settings, builds the Pinecone client,
// describes the index to validate its dimension against the configured one,
// and opens the data-plane connection — so a bad API key, a missing index, or
// a dimension mismatch all fail at startup rather than on first use.
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
	if set.Dimension <= 0 {
		return fmt.Errorf("pinecone connector %q: dimension is required", config.Name)
	}

	params := sdk.NewClientParams{ApiKey: set.APIKey}
	if set.Host != "" {
		params.Host = set.Host
	}
	client, err := sdk.NewClient(params)
	if err != nil {
		return fmt.Errorf("pinecone connector %q: %w", config.Name, err)
	}

	idx, err := client.DescribeIndex(ctx, set.Index)
	if err != nil {
		return fmt.Errorf("pinecone connector %q: describe index %q: %w", config.Name, set.Index, err)
	}
	if err := checkDimension(idx, set.Dimension); err != nil {
		return fmt.Errorf("pinecone connector %q: %w", config.Name, err)
	}

	idxConn, err := client.Index(sdk.NewIndexConnParams{Host: idx.Host, Namespace: set.Namespace})
	if err != nil {
		return fmt.Errorf("pinecone connector %q: connect to index %q: %w", config.Name, set.Index, err)
	}

	c.client = client
	c.idxConn = idxConn
	slog.Info("pinecone connector started",
		"connector", config.Name, "index", set.Index, "dimension", set.Dimension)
	return nil
}

// checkDimension compares an index's actual dimension against the configured
// one. Split out from Start so the check is testable without a live index.
func checkDimension(idx *sdk.Index, configured int) error {
	if idx.Dimension == nil {
		return fmt.Errorf("index %q has no dimension (sparse indexes are not supported)", idx.Name)
	}
	if int(*idx.Dimension) != configured {
		return fmt.Errorf("index %q has dimension %d, configured dimension is %d", idx.Name, *idx.Dimension, configured)
	}
	return nil
}

// Stop closes the data-plane connection opened by Start.
func (c *Connector) Stop(context.Context) error {
	if c.idxConn == nil {
		return nil
	}
	err := c.idxConn.Close()
	c.idxConn = nil
	if err != nil {
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
func (c *Connector) IndexConnection(namespace string) *sdk.IndexConnection {
	if namespace == "" {
		return c.idxConn
	}
	return c.idxConn.WithNamespace(namespace)
}
