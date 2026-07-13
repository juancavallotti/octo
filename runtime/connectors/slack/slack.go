// Package slack integrates Slack with flows. Its connector holds the bot
// credentials and talks to the Slack Web API; the blocks it registers send
// messages, verify inbound event requests, filter and normalize events, and
// enrich a message with Slack data.
//
// Inbound Slack events arrive over the http connector (Slack posts JSON to a
// route it owns); the slack-verify-request and slack-event blocks process that
// request. Signature verification runs over the exact request bytes, which the
// http source exposes via its rawBodyVar setting.
package slack

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
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterConnector("slack", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the slack connector and every slack-* block
	// share the Slack palette group and icon unless they set their own.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: displayName})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "slack",
		Label:    displayName,
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// displayName is the editor-facing label, palette group, and icon for the slack
// connector and its blocks.
const displayName = "Slack"

const (
	defaultAPIBaseURL = "https://slack.com/api"
	defaultTimeout    = 30 * time.Second
	// signatureVersion is the version prefix Slack signs its requests with.
	signatureVersion = "v0"
	// maxTimestampSkew bounds how far a signed request's timestamp may be from
	// now, to limit replay of a captured signature.
	maxTimestampSkew = 5 * time.Minute
)

// connectorSettings is the global config decoded from the connector's settings.
type connectorSettings struct {
	// Bot User OAuth token (xoxb-...) used to call the Web API.
	BotToken string `json:"botToken" octo:"label=Bot token,required"`
	// Verifies inbound Slack request signatures; required to receive events.
	SigningSecret string `json:"signingSecret" octo:"label=Signing secret"`
	// APIBaseURL overrides the Slack Web API base (default https://slack.com/api),
	// mainly so tests can point at a stub server. Not exposed in the editor schema.
	APIBaseURL string `json:"apiBaseURL"`
	// Bounds each Slack Web API call.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=30s"`
}

// Connector holds Slack credentials and an HTTP client for the Slack Web API.
// Blocks resolve it by name and either call the Web API through Call or verify
// an inbound request through VerifySignature. It is safe for concurrent use:
// *http.Client is, and the credentials are read-only after Start.
type Connector struct {
	client        *http.Client
	baseURL       string
	botToken      string
	signingSecret string
}

// Start decodes the settings and builds the Web API client. A bot token is
// required; the signing secret is optional here and validated by the verify
// block when a flow actually receives events.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.BotToken) == "" {
		return errors.New("slack connector requires a \"botToken\" setting")
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
	c.botToken = set.BotToken
	c.signingSecret = set.SigningSecret
	c.client = &http.Client{Timeout: timeout}
	return nil
}

// Stop releases nothing: the HTTP client needs no shutdown.
func (c *Connector) Stop(context.Context) error { return nil }

// Call POSTs a JSON payload to a Slack Web API method (e.g. "chat.postMessage")
// and returns the decoded response envelope. Slack signals application errors
// with an "ok": false body carrying an "error" code, which Call surfaces as a
// Go error (the decoded body is still returned for context). The bot token
// authenticates the request.
func (c *Connector) Call(ctx context.Context, method string, payload any) (map[string]any, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("slack %s: encode payload: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("slack %s: build request: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+c.botToken)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("slack %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("slack %s: decode response: %w", method, err)
	}
	if ok, _ := decoded["ok"].(bool); !ok {
		code, _ := decoded["error"].(string)
		if code == "" {
			code = "unknown_error"
		}
		return decoded, fmt.Errorf("slack %s: %s", method, code)
	}
	return decoded, nil
}

// SigningSecret returns the configured signing secret; it is empty when the
// connector was set up for sending only.
func (c *Connector) SigningSecret() string { return c.signingSecret }

// VerifySignature reports whether sig is Slack's valid signature for rawBody at
// the given timestamp. It requires a configured signing secret, rejects a
// timestamp outside maxTimestampSkew (bounding replay), and compares in constant
// time. now is passed in so callers (and tests) control the clock.
func (c *Connector) VerifySignature(sig, timestamp string, rawBody []byte, now time.Time) bool {
	if c.signingSecret == "" || sig == "" || timestamp == "" {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if delta := now.Sub(time.Unix(ts, 0)); delta > maxTimestampSkew || delta < -maxTimestampSkew {
		return false
	}

	mac := hmac.New(sha256.New, []byte(c.signingSecret))
	// hash.Hash.Write never errors; the assignment satisfies errcheck.
	_, _ = mac.Write([]byte(signatureVersion + ":" + timestamp + ":" + string(rawBody)))
	expected := signatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
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
