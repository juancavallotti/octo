// Package mailer sends transactional email through Resend. It is the orchestrator's
// only outbound email path and knows nothing about where the credentials came from —
// the caller supplies the key with each message, so the package has no configuration,
// no lifecycle, and nothing to keep in sync with the settings that own the key.
package mailer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// resendEndpoint is the provider's send API.
	resendEndpoint = "https://api.resend.com/emails"
	// sendTimeout bounds one send. Applied to the request context rather than the
	// client, so a caller's shorter deadline still wins.
	sendTimeout = 15 * time.Second
	// maxResponseBytes caps the provider's response. Success is a short JSON object
	// and failures are not much longer; anything larger is not something to read into
	// memory on a provider's say-so.
	maxResponseBytes = 1 << 16
)

// Message is one email to send. From is pre-composed by the caller (either a bare
// address or "Name <address>"), because the display name belongs to the site's
// identity settings rather than to this package.
type Message struct {
	APIKey  string
	From    string
	ReplyTo string
	To      []string
	Subject string
	Text    string
	HTML    string
}

// Resend sends through the Resend API.
type Resend struct {
	client   *http.Client
	endpoint string
}

// NewResend returns a Resend client pointed at the production API.
func NewResend() *Resend {
	return newResend(resendEndpoint)
}

// newResend returns a client pointed at endpoint. The seam exists for the tests,
// which run against an httptest server.
func newResend(endpoint string) *Resend {
	// No global Timeout: the bound is per-send and travels on the request context, so
	// a caller with a tighter deadline is not overridden by a looser one here.
	return &Resend{client: &http.Client{}, endpoint: endpoint}
}

// sendRequest is the provider's wire shape. Resend uses snake_case, and accepts
// either or both of text/html — the caller guarantees at least one is set.
type sendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text,omitempty"`
	HTML    string   `json:"html,omitempty"`
	ReplyTo string   `json:"reply_to,omitempty"`
}

// sendResponse is the success shape: the provider's id for the queued message.
type sendResponse struct {
	ID string `json:"id"`
}

// errorResponse is the failure shape. Both fields are best-effort — a gateway in
// front of the API can return something else entirely, which is why every use of
// this is guarded by whether the decode succeeded.
type errorResponse struct {
	Message string `json:"message"`
	Name    string `json:"name"`
}

// Send delivers m and returns the provider's message id.
func (r *Resend) Send(ctx context.Context, m Message) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	body, err := json.Marshal(sendRequest{
		From:    m.From,
		To:      m.To,
		Subject: m.Subject,
		Text:    m.Text,
		HTML:    m.HTML,
		ReplyTo: m.ReplyTo,
	})
	if err != nil {
		return "", fmt.Errorf("mailer: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("mailer: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		// A transport failure and a timeout are the same fact for the caller: the
		// provider did not answer, and the message may or may not have been sent.
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", statusError(resp.StatusCode, resp.Status, respBody)
	}
	if readErr != nil {
		return "", fmt.Errorf("%w: read response: %w", ErrUnavailable, readErr)
	}

	var out sendResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("%w: decode response: %w", ErrUnavailable, err)
	}
	return out.ID, nil
}

// statusError maps a non-2xx response to one of the package's three sentinels,
// carrying the provider's own message through when there is one. That message is
// worth preserving verbatim: "The acme.com domain is not verified" names both the
// problem and the domain, which no wording invented here could.
func statusError(code int, status string, body []byte) error {
	detail := status
	var parsed errorResponse
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Message != "" {
		detail = parsed.Message
	}

	switch {
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrInvalidAPIKey, detail)
	case code >= 400 && code < 500:
		return fmt.Errorf("%w: %s", ErrRejected, detail)
	default:
		return fmt.Errorf("%w: %s", ErrUnavailable, detail)
	}
}
