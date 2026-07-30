// Package notion integrates Notion with flows. Its connector holds the
// integration token and talks to the Notion API; the blocks it registers
// retrieve a page, query a data source, verify and normalize inbound webhooks,
// and render a page's blocks to markdown.
//
// Inbound Notion webhooks arrive over the http connector (Notion posts JSON to a
// route it owns); the notion-verify-request and notion-event blocks process that
// request. Signature verification runs over the exact request bytes, which the
// http source exposes via its rawBodyVar setting.
package notion

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
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
	registerVerify()
	registerEvent()
	registerRetrievePage()
	registerRetrieveBlocks()
	registerPageToMarkdown()
	registerQueryDataSource()
}

func registerConnector() {
	core.MustRegisterConnector("notion", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the notion connector and every notion-* block
	// share the Notion palette group and icon unless they set their own.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: displayName})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "notion",
		Label:    displayName,
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// displayName is the editor-facing label, palette group, and icon for the notion
// connector and its blocks.
const displayName = "Notion"

const (
	defaultAPIBaseURL = "https://api.notion.com/v1"
	// defaultNotionVersion pins the Notion-Version header. The data-sources query
	// API (data_sources/{id}/query) requires 2025-09-03 or later.
	defaultNotionVersion = "2025-09-03"
	defaultTimeout       = 30 * time.Second
	// signaturePrefix is the scheme Notion prefixes its webhook signatures with.
	signaturePrefix = "sha256="
)

// connectorSettings is the global config decoded from the connector's settings.
type connectorSettings struct {
	// Notion integration token used as a Bearer credential for the API.
	Token string `json:"token" octo:"label=Integration token,required"`
	// Notion-Version header sent on every request; data_sources query requires
	// 2025-09-03 or later.
	NotionVersion string `json:"notionVersion" octo:"label=Notion version,default=2025-09-03"`
	// Verifies inbound Notion webhook signatures; required to receive events.
	VerificationToken string `json:"verificationToken" octo:"label=Verification token"`
	// APIBaseURL overrides the Notion API base (default https://api.notion.com/v1),
	// mainly so tests can point at a stub server. Not exposed in the editor schema.
	APIBaseURL string `json:"apiBaseURL"`
	// Bounds each Notion API call.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=30s"`
}

// Connector holds Notion credentials and an HTTP client for the Notion API.
// Blocks resolve it by name and either call the API through Call or verify an
// inbound webhook through VerifySignature. It is safe for concurrent use:
// *http.Client is, the credentials are read-only after Start, and the one field
// that is not — the token captured from a subscription handshake — is guarded by
// mu, since request goroutines verify concurrently.
type Connector struct {
	client            *http.Client
	baseURL           string
	token             string
	version           string
	verificationToken string

	// mu guards capturedToken, the verification token learned at runtime from
	// Notion's subscription handshake when none was configured.
	mu            sync.RWMutex
	capturedToken string
}

// Start decodes the settings and builds the API client. A token is required; the
// verification token is optional here and validated by the verify block when a
// flow actually receives webhooks.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.Token) == "" {
		return errors.New("notion connector requires a \"token\" setting")
	}

	timeout := time.Duration(set.Timeout)
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	base := strings.TrimSpace(set.APIBaseURL)
	if base == "" {
		base = defaultAPIBaseURL
	}
	version := normalizeVersion(set.NotionVersion)
	if version == "" {
		version = defaultNotionVersion
	}

	c.baseURL = strings.TrimRight(base, "/")
	c.token = set.Token
	c.version = version
	c.verificationToken = set.VerificationToken
	// A pooled transport: a nil one means http.DefaultTransport, whose idle pool
	// is two connections per host — see httppool.
	c.client = httppool.NewClient(timeout)
	return nil
}

// Stop releases nothing: the HTTP client needs no shutdown.
func (c *Connector) Stop(context.Context) error { return nil }

// Call sends a JSON request to a Notion API path (e.g. "pages/{id}" or
// "data_sources/{id}/query") with the given HTTP method and returns the decoded
// response body. A nil payload sends no body (for GET). Notion signals errors
// with an HTTP status >= 400 carrying an {object:"error", code, message} body,
// which Call surfaces as a Go error (the decoded body is still returned for
// context). The integration token and Notion-Version authenticate the request.
func (c *Connector) Call(ctx context.Context, httpMethod, path string, payload any) (map[string]any, error) {
	var reader *bytes.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("notion %s: encode payload: %w", path, err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, httpMethod, c.baseURL+"/"+path, reader)
	if err != nil {
		return nil, fmt.Errorf("notion %s: build request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Notion-Version", c.version)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("notion %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("notion %s: decode response: %w", path, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		code, _ := decoded["code"].(string)
		message, _ := decoded["message"].(string)
		if code == "" {
			code = strconv.Itoa(resp.StatusCode)
		}
		if message != "" {
			return decoded, fmt.Errorf("notion %s: %s: %s", path, code, message)
		}
		return decoded, fmt.Errorf("notion %s: %s", path, code)
	}
	return decoded, nil
}

// VerificationToken returns the webhook verification token in effect: the
// configured one, else the one captured from a subscription handshake. It is empty
// only while neither exists — the connector was set up for outbound calls only, or
// a fresh subscription has yet to shake hands.
func (c *Connector) VerificationToken() string {
	if c.verificationToken != "" {
		return c.verificationToken
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.capturedToken
}

// CaptureVerificationToken remembers the token Notion sent in its one-time
// subscription handshake, so the rest of this process can verify real events
// against it without a restart. It does not survive one: the token is logged for
// the operator to persist as NOTION_VERIFICATION_TOKEN. A configured token always
// wins, so capturing cannot displace it.
func (c *Connector) CaptureVerificationToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capturedToken = token
}

// VerifySignature reports whether sig is Notion's valid signature for rawBody. It
// requires a verification token to be in effect, and compares in constant time.
// Notion signs the exact request bytes with HMAC-SHA256 keyed by the verification
// token and prefixes the hex digest with "sha256=".
func (c *Connector) VerifySignature(sig string, rawBody []byte) bool {
	token := c.VerificationToken()
	if token == "" || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(token))
	// hash.Hash.Write never errors; the assignment satisfies errcheck.
	_, _ = mac.Write(rawBody)
	expected := signaturePrefix + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

// normalizeVersion cleans a configured Notion-Version. Notion wants a bare date
// (e.g. "2025-09-03"), but a date written unquoted in YAML is parsed as a
// timestamp and round-trips through settings' JSON as an RFC3339 string
// ("2025-09-03T00:00:00Z"); keep only the leading date so the header validates.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, 'T'); i > 0 {
		return v[:i]
	}
	return v
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
