// Package tavily integrates Tavily's agentic search with flows. Its connector
// holds the API key and talks to the Tavily API; the blocks it registers search
// the live web, extract clean content from known URLs, crawl a site, and map a
// site's link graph.
//
// Every Tavily endpoint is a JSON POST authenticated with a bearer key, so the
// connector exposes one Call for all of them and each block owns the payload it
// builds. Crawl and map run server-side for up to 150s — well past the
// connector's default timeout — so a flow that uses them must raise it.
package tavily

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	registerSearch()
}

func registerConnector() {
	core.MustRegisterConnector("tavily", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the tavily connector and every tavily-* block
	// share the Tavily palette group and brand icon unless they set their own.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: displayName})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "tavily",
		Label:    displayName,
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// displayName is the editor-facing label, palette group, and icon for the tavily
// connector and its blocks.
const displayName = "Tavily"

const (
	defaultAPIBaseURL = "https://api.tavily.com"
	defaultTimeout    = 30 * time.Second
)

// connectorSettings is the global config decoded from the connector's settings.
type connectorSettings struct {
	// Authenticates with the Tavily API; source from ${TAVILY_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// APIBaseURL overrides the Tavily API base (default https://api.tavily.com),
	// mainly so tests can point at a stub server. Not exposed in the editor schema.
	APIBaseURL string `json:"apiBaseURL"`
	// Bounds each Tavily API call. Raise it for tavily-crawl and tavily-map, which
	// Tavily runs server-side for up to 150s.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=30s"`
}

// Connector holds the Tavily credential and an HTTP client for the Tavily API.
// Blocks resolve it by name and call the API through Call. It is safe for
// concurrent use: *http.Client is, and the credentials are read-only after Start.
type Connector struct {
	client  *http.Client
	baseURL string
	apiKey  string
}

// Start decodes the settings and builds the API client. Tavily has no cheap
// "who am I" endpoint, so the key is validated for presence only — a bad key
// surfaces on the first call as a 401.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return errors.New("tavily connector requires an \"apiKey\" setting")
	}

	timeout := time.Duration(set.Timeout)
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	base := strings.TrimSpace(set.APIBaseURL)
	if base == "" {
		base = defaultAPIBaseURL
	}

	c.baseURL = strings.TrimRight(base, "/")
	c.apiKey = set.APIKey
	// A pooled transport: a nil one means http.DefaultTransport, whose idle pool
	// is two connections per host — see httppool.
	c.client = httppool.NewClient(timeout)
	return nil
}

// Stop releases nothing: the HTTP client needs no shutdown.
func (c *Connector) Stop(context.Context) error { return nil }

// Call posts a JSON payload to a Tavily API path ("search", "extract", "crawl",
// "map") and returns the decoded response body. Tavily signals errors with an
// HTTP status >= 400 carrying a {detail: {error}} body, which Call surfaces as a
// Go error; the decoded body is still returned for context.
func (c *Connector) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	if c.client == nil {
		return nil, fmt.Errorf("tavily %s: connector not started", path)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tavily %s: encode payload: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("tavily %s: build request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("tavily %s: decode response: %w", path, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return decoded, fmt.Errorf("tavily %s: %s", path, apiErrorMessage(decoded, resp.StatusCode))
	}
	return decoded, nil
}

// apiErrorMessage digs Tavily's error text out of a failed response. Tavily
// nests it under detail.error, but returns a bare {error} or a plain {detail}
// string on some paths, so all three are tried before falling back to the status
// code.
func apiErrorMessage(body map[string]any, status int) string {
	if detail, ok := body["detail"].(map[string]any); ok {
		if msg, _ := detail["error"].(string); msg != "" {
			return msg
		}
	}
	if msg, _ := body["detail"].(string); msg != "" {
		return msg
	}
	if msg, _ := body["error"].(string); msg != "" {
		return msg
	}
	return strconv.Itoa(status)
}

// duration decodes either a Go duration string ("5s") or a numeric nanosecond
// count from settings, since settings round-trip through JSON.
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
