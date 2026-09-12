// Package iam talks to the iam service, which is where platform tokens come from.
//
// One operation: lending a deployment an identity of its own. The caller's own
// token authorises it, and iam decides whether that caller may — this package
// carries the request and nothing more.
package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// requestTimeout bounds a mint. It happens while somebody is waiting for a
// deployment to be created, so it cannot be generous.
const requestTimeout = 10 * time.Second

// Client is an iam address to send to. The zero value is a client with nowhere
// to send, which reports itself through Configured and mints nothing.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client for baseURL. An empty address is not an error: an install
// with no iam configured deploys pods with no identity, exactly as it did before
// there were any.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// Configured reports whether this client has an address to reach iam at.
func (c *Client) Configured() bool { return c != nil && c.baseURL != "" }

// MintMachine asks iam for a token belonging to deployment, on the authority of
// the caller's own token.
//
// `access` is what the deployment is being lent beyond its own stores; empty
// lends nothing more. It is a request, not an instruction: iam decides whether
// this caller may lend it and refuses if not.
//
// The token comes back opaque and is not inspected here: what it may reach is
// iam's decision, stamped into claims the orchestrator verifies at the other end
// rather than a shape this caller chose.
func (c *Client) MintMachine(
	ctx context.Context, callerToken, deployment string, access []string,
) (string, error) {
	if !c.Configured() {
		return "", fmt.Errorf("iam: no address is configured")
	}
	if strings.TrimSpace(callerToken) == "" {
		return "", fmt.Errorf("iam: the caller presented no token to mint on the authority of")
	}

	request := map[string]any{"deployment": deployment}
	if len(access) > 0 {
		request["access"] = access
	}
	body, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("iam: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/auth/machine", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("iam: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+callerToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("iam: mint a machine token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// iam's own wording, which says whether the caller may not deploy or was
		// simply not recognised — the two an operator has to tell apart.
		return "", fmt.Errorf("iam: mint a machine token: %s: %s",
			resp.Status, strings.TrimSpace(reason(resp)))
	}

	var answer struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return "", fmt.Errorf("iam: read the minted token: %w", err)
	}
	if answer.Token == "" {
		return "", fmt.Errorf("iam: mint a machine token: the reply carried no token")
	}
	return answer.Token, nil
}

// reason reads the error iam reported, or "" when the body is not one.
func reason(resp *http.Response) string {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ""
	}
	return body.Error
}
