// Package parallel integrates Parallel's web research APIs with flows. Its
// connector holds the API key and the webhook secret; the blocks it registers run
// a synchronous search, start an asynchronous task run, and authenticate the
// callback that run delivers.
//
// The two halves are deliberately different shapes. Search answers in the same
// request. A task run does not: it returns a handle, and the answer arrives later
// as a webhook posted to a route the flow owns, over the http connector — which
// is why this connector also verifies signatures, the way slack and notion do.
package parallel

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	registerTaskRun()
	registerVerify()
}

func registerConnector() {
	core.MustRegisterConnector("parallel", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the parallel connector and every parallel-*
	// block share the Parallel palette group and brand icon unless they set their
	// own.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: displayName})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "parallel",
		Label:    displayName,
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// displayName is the editor-facing label, palette group, and icon for the
// parallel connector and its blocks.
const displayName = "Parallel"

const (
	defaultAPIBaseURL = "https://api.parallel.ai"
	defaultTimeout    = 30 * time.Second
	// secretPrefix is the Standard Webhooks marker saying the rest of the secret
	// is base64-encoded key material rather than the key itself.
	secretPrefix = "whsec_"
	// signatureVersion is the only Standard Webhooks signature scheme defined, and
	// the only one this connector accepts.
	signatureVersion = "v1"
	// maxTimestampSkew bounds how far a signed webhook's timestamp may be from
	// now, to limit replay of a captured signature. It matches slack's window.
	maxTimestampSkew = 5 * time.Minute
)

// VerifySignature reports whether sig authenticates rawBody as the webhook
// delivered with the given id and timestamp. It implements Standard Webhooks:
// HMAC-SHA256 over "<id>.<timestamp>.<body>" keyed by the decoded secret, the
// result base64-encoded and compared in constant time.
//
// now is passed in so callers (and tests) control the clock.
func (c *Connector) VerifySignature(id, timestamp, sig string, rawBody []byte, now time.Time) bool {
	if len(c.webhookKey) == 0 || id == "" || timestamp == "" || sig == "" {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if delta := now.Sub(time.Unix(ts, 0)); delta > maxTimestampSkew || delta < -maxTimestampSkew {
		return false
	}

	mac := hmac.New(sha256.New, c.webhookKey)
	// hash.Hash.Write never errors; the assignment satisfies errcheck.
	_, _ = mac.Write([]byte(id + "." + timestamp + "." + string(rawBody)))
	expected := []byte(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	return matchesAny(expected, sig)
}

// matchesAny reports whether any versioned signature in the header matches
// expected. The header is a space-delimited list — Parallel sends more than one
// while a secret is being rotated, and both are valid — so every entry is
// compared, without short-circuiting on the first match.
func matchesAny(expected []byte, header string) bool {
	var matched bool
	for _, entry := range strings.Fields(header) {
		version, signature, ok := strings.Cut(entry, ",")
		if !ok || version != signatureVersion {
			continue
		}
		if hmac.Equal(expected, []byte(signature)) {
			matched = true
		}
	}
	return matched
}

// connectorSettings is the global config decoded from the connector's settings.
type connectorSettings struct {
	// Authenticates with the Parallel API; source from ${PARALLEL_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Verifies inbound task-run webhooks; required to receive them. Take it from
	// Settings -> Webhooks on the Parallel platform, whsec_ prefix included.
	WebhookSecret string `json:"webhookSecret" octo:"label=Webhook secret"`
	// APIBaseURL overrides the Parallel API base (default https://api.parallel.ai),
	// mainly so tests can point at a stub server. Not exposed in the editor schema.
	APIBaseURL string `json:"apiBaseURL"`
	// Bounds each Parallel API call.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=30s"`
}

// Connector holds Parallel credentials and an HTTP client for the Parallel API.
// Blocks resolve it by name and either call the API through Call or authenticate
// an inbound webhook through VerifySignature. It is safe for concurrent use:
// *http.Client is, and every other field is read-only after Start.
type Connector struct {
	client  *http.Client
	baseURL string
	apiKey  string
	// webhookKey is the decoded HMAC key, not the configured string: a Standard
	// Webhooks secret carries base64 key material behind a whsec_ prefix, and
	// decoding it once at Start is what turns a malformed secret into a startup
	// failure instead of a signature that silently never matches.
	webhookKey []byte
}

// Start decodes the settings and builds the API client. The webhook secret is
// optional here — a flow may only ever call the API — but a malformed one is not:
// it is decoded now so it fails at startup rather than on the first callback.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return errors.New("parallel connector requires an \"apiKey\" setting")
	}
	key, err := decodeWebhookSecret(set.WebhookSecret)
	if err != nil {
		return err
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
	c.webhookKey = key
	// A pooled transport: a nil one means http.DefaultTransport, whose idle pool
	// is two connections per host — see httppool.
	c.client = httppool.NewClient(timeout)
	return nil
}

// decodeWebhookSecret turns a configured secret into HMAC key material. A
// whsec_-prefixed secret carries base64 key material, which Standard Webhooks
// says to decode before signing; anything else is used as raw bytes, which is
// what a secret pasted without its prefix means.
func decodeWebhookSecret(secret string) ([]byte, error) {
	secret = strings.TrimSpace(secret)
	switch {
	case secret == "":
		return nil, nil
	case strings.HasPrefix(secret, secretPrefix):
		key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, secretPrefix))
		if err != nil {
			return nil, fmt.Errorf("parallel connector: webhookSecret after %q is not base64: %w", secretPrefix, err)
		}
		return key, nil
	default:
		return []byte(secret), nil
	}
}

// Stop releases nothing: the HTTP client needs no shutdown.
func (c *Connector) Stop(context.Context) error { return nil }

// HasWebhookSecret reports whether a webhook secret was configured. The verify
// block asks at build time so a flow that cannot authenticate its callbacks fails
// to start rather than at the first one.
func (c *Connector) HasWebhookSecret() bool { return len(c.webhookKey) > 0 }

// Call posts a JSON payload to a Parallel API path ("v1/search",
// "v1/tasks/runs") and returns the decoded response body. Parallel signals errors
// with an HTTP status >= 400 carrying a {detail} body, which Call surfaces as a
// Go error; the decoded body is still returned for context.
func (c *Connector) Call(ctx context.Context, path string, payload any) (map[string]any, error) {
	if c.client == nil {
		return nil, fmt.Errorf("parallel %s: connector not started", path)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("parallel %s: encode payload: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parallel %s: build request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("parallel %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	var decoded map[string]any
	// Decode before branching on status, but do not let a decode failure decide
	// the error: a gateway in front of the API answers 429/502 with an HTML page
	// or nothing at all, and reporting "invalid character '<'" would throw away
	// the status code, which is the useful half.
	decodeErr := json.NewDecoder(resp.Body).Decode(&decoded)
	if resp.StatusCode >= http.StatusBadRequest {
		return decoded, fmt.Errorf("parallel %s: %s", path, apiErrorMessage(decoded, resp.StatusCode))
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("parallel %s: decode response: %w", path, decodeErr)
	}
	return decoded, nil
}

// apiErrorMessage digs Parallel's error text out of a failed response.
//
// The shape that matters most is {error: {message, detail: {errors: [...]}}}, a
// request-validation failure. Its top-level message is always the same sentence,
// so the useful part is the per-field list underneath — without it a rejected
// field reads as a bare "422" and says nothing about which field was wrong.
func apiErrorMessage(body map[string]any, status int) string {
	if wrapped, ok := body["error"].(map[string]any); ok {
		if msg := wrappedErrorMessage(wrapped); msg != "" {
			return msg
		}
	}
	if msg, _ := body["error"].(string); msg != "" {
		return msg
	}
	switch detail := body["detail"].(type) {
	case string:
		if detail != "" {
			return detail
		}
	case map[string]any:
		for _, key := range []string{"message", "error", "type"} {
			if msg, _ := detail[key].(string); msg != "" {
				return msg
			}
		}
	}
	return strconv.Itoa(status)
}

// wrappedErrorMessage renders an {message, detail:{errors}} error object, keeping
// the per-field list when there is one.
func wrappedErrorMessage(wrapped map[string]any) string {
	message, _ := wrapped["message"].(string)
	fields := validationFields(wrapped)
	switch {
	case message != "" && fields != "":
		return message + " (" + fields + ")"
	case fields != "":
		return fields
	default:
		return message
	}
}

// validationFields renders a validation error's per-field entries as
// "body.max_results: Extra inputs are not permitted; ...", naming what to change.
func validationFields(wrapped map[string]any) string {
	detail, ok := wrapped["detail"].(map[string]any)
	if !ok {
		return ""
	}
	entries, ok := detail["errors"].([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		msg, _ := entry["msg"].(string)
		where := joinLoc(entry["loc"])
		switch {
		case where != "" && msg != "":
			parts = append(parts, where+": "+msg)
		case msg != "":
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}

// joinLoc flattens a validation error's loc path (["body","max_results"]) into
// "body.max_results".
func joinLoc(raw any) string {
	items, ok := raw.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ".")
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
