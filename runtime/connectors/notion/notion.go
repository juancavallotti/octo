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
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterConnector("notion", func() core.Connector {
		return &Connector{}
	})
}

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
	// Token is the Notion integration token (secret / ntn_...) used to call the
	// API as an Authorization: Bearer credential.
	Token string `json:"token"`
	// NotionVersion sets the Notion-Version header on every request (default
	// 2025-09-03).
	NotionVersion string `json:"notionVersion"`
	// VerificationToken verifies the signature on inbound Notion webhooks. It is
	// only needed by flows that receive events; the verify block requires it.
	VerificationToken string `json:"verificationToken"`
	// APIBaseURL overrides the Notion API base (default https://api.notion.com/v1),
	// mainly so tests can point at a stub server.
	APIBaseURL string `json:"apiBaseURL"`
	// Timeout bounds each API call (default 30s).
	Timeout duration `json:"timeout"`
}

// Connector holds Notion credentials and an HTTP client for the Notion API.
// Blocks resolve it by name and either call the API through Call or verify an
// inbound webhook through VerifySignature. It is safe for concurrent use:
// *http.Client is, and the credentials are read-only after Start.
type Connector struct {
	client            *http.Client
	baseURL           string
	token             string
	version           string
	verificationToken string
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
	version := strings.TrimSpace(set.NotionVersion)
	if version == "" {
		version = defaultNotionVersion
	}

	c.baseURL = strings.TrimRight(base, "/")
	c.token = set.Token
	c.version = version
	c.verificationToken = set.VerificationToken
	c.client = &http.Client{Timeout: timeout}
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

// VerificationToken returns the configured webhook verification token; it is
// empty when the connector was set up for outbound calls only.
func (c *Connector) VerificationToken() string { return c.verificationToken }

// VerifySignature reports whether sig is Notion's valid signature for rawBody. It
// requires a configured verification token, and compares in constant time. Notion
// signs the exact request bytes with HMAC-SHA256 keyed by the verification token
// and prefixes the hex digest with "sha256=".
func (c *Connector) VerifySignature(sig string, rawBody []byte) bool {
	if c.verificationToken == "" || sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.verificationToken))
	// hash.Hash.Write never errors; the assignment satisfies errcheck.
	_, _ = mac.Write(rawBody)
	expected := signaturePrefix + hex.EncodeToString(mac.Sum(nil))
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
