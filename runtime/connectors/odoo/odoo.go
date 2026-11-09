// Package odoo integrates Odoo with flows. Its connector holds an API key and
// talks to an Odoo server through a client chosen by the server's version: Odoo
// 19 and later speak the JSON-2 API, Odoo 18 and earlier the legacy JSON-RPC
// endpoint. The blocks it registers see one interface over both — a call names a
// model, a method, optional record ids and named arguments — so every model of
// every installed module, custom ones included, is reachable the same way.
package odoo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/connectors/internal/httppool"
	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
}

const connectorType = "odoo"

func registerConnector() {
	core.MustRegisterConnector(connectorType, func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the odoo connector and every odoo-* block
	// share the Odoo palette group and icon unless they set their own.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: displayName})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     connectorType,
		Label:    displayName,
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// displayName is the editor-facing label, palette group, and icon for the odoo
// connector and its blocks.
const displayName = "Odoo"

const (
	// version19 selects the JSON-2 API, introduced in Odoo 19.
	version19 = "19"

	defaultTimeout = 30 * time.Second
)

// connectorSettings is the global config decoded from the connector's settings.
type connectorSettings struct {
	// Odoo server version, which selects the protocol: 19 uses the JSON-2 API.
	Version string `json:"version" octo:"label=Odoo version,type=enum,enum=19,default=19"`
	// Base URL of the Odoo server, e.g. https://mycompany.odoo.com.
	URL string `json:"url" octo:"label=Server URL,required"`
	// API key of the Odoo user the flow acts as.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Database to use. Required for version 18; for version 19 only when the
	// server hosts more than one database.
	Database string `json:"database" octo:"label=Database"`
	// Login of the user the API key belongs to. Required for version 18.
	Username string `json:"username" octo:"label=Username"`
	// Bounds each Odoo call.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=30s"`
}

// Connector holds the Odoo client for its configured version. Blocks resolve it
// by name and call through Call. It is safe for concurrent use: both clients are.
type Connector struct {
	client caller
}

// Start decodes the settings, checks the combination the chosen version needs,
// and builds that version's client. It makes no request: a bad key surfaces on
// the first call, as a flow error.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	base := strings.TrimRight(strings.TrimSpace(set.URL), "/")
	if base == "" {
		return errors.New("odoo connector requires a \"url\" setting")
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return errors.New("odoo connector requires an \"apiKey\" setting")
	}
	timeout := time.Duration(set.Timeout)
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	// A pooled transport: a nil one means http.DefaultTransport, whose idle pool
	// is two connections per host — see httppool.
	httpClient := httppool.NewClient(timeout)

	switch strings.TrimSpace(set.Version) {
	case "", version19:
		c.client = &json2Client{http: httpClient, baseURL: base, apiKey: set.APIKey, database: set.Database}
	default:
		return fmt.Errorf("odoo connector: unsupported version %q (want 19)", set.Version)
	}
	return nil
}

// Stop releases nothing: the HTTP client needs no shutdown.
func (c *Connector) Stop(context.Context) error { return nil }

// Call invokes a method on an Odoo model and returns its decoded result. A
// failure Odoo reports — access denied, a validation error, an unknown method —
// is an *Error whichever version produced it.
func (c *Connector) Call(ctx context.Context, req Request) (any, error) {
	if req.Model == "" || req.Method == "" {
		return nil, errors.New("odoo call requires a model and a method")
	}
	return c.client.call(ctx, req)
}

// duration decodes a connector timeout from a string ("30s") or nanoseconds.
type duration time.Duration

// UnmarshalJSON parses a duration from a quoted string ("250ms") or a number.
func (d *duration) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		parsed, err := time.ParseDuration(strings.Trim(s, `"`))
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		*d = duration(parsed)
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parse duration: %w", err)
	}
	*d = duration(n)
	return nil
}
