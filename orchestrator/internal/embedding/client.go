// Package embedding turns text into vectors by asking the embedding server for
// them.
//
// It holds no credentials and knows no providers. Both used to live here — a
// provider switch with one HTTP client per API, and an encrypted key in
// site_settings behind an admin page — and both were the wrong home for two
// separate reasons.
//
// The provider code was a second implementation. The runtime already knows how
// to call OpenAI, Gemini and OpenRouter embeddings endpoints, and how to bill
// the call: that is the `ai-embed` block and its connectors. A Go copy of it in
// the orchestrator was the same knowledge written twice, with its own switch to
// keep in step as providers change.
//
// The settings page was worse. Changing the model is not something an operator
// may do — vectors carry no record of which model produced them, so a store
// holding two models' cannot be ranked coherently — and a control that must
// never be touched has no business behind a Save button. It is deploy-time
// configuration, so it is configuration of the deployment: chart values on the
// embedding server, which holds the key in exactly one pod.
//
// What is left here is the client: one URL, one POST.
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// requestTimeout bounds one call to the embedding server. Generous because a
// batch of sixty-four texts is one upstream provider call behind it, and the
// sweep would rather wait than retry a batch the provider is already working on.
const requestTimeout = 60 * time.Second

// statusTimeout bounds the health probe, which does no provider work at all and
// so has no reason to be slow. Short enough that an admin page waiting on it
// does not itself become the thing that feels broken.
const statusTimeout = 3 * time.Second

// maxErrorBody caps how much of a failing response is quoted back. Enough for a
// provider's error message, bounded so a stray HTML page does not land in a log
// line.
const maxErrorBody = 2 << 10

// Client calls the embedding server.
//
// The zero-value URL means no embedding server was deployed, which is a
// supported way to run rather than a fault: searching agent memory then matches
// text instead of ranking by meaning. Every method answers accordingly rather
// than erroring, so no caller has to branch on whether the installation has one.
type Client struct {
	url  string
	http *http.Client
}

// NewClient returns a client for the embedding server at url. An empty url
// yields a client that reports itself unconfigured.
func NewClient(url string) *Client {
	return &Client{
		url:  strings.TrimRight(strings.TrimSpace(url), "/"),
		http: &http.Client{Timeout: requestTimeout},
	}
}

// FromEnv builds the client from EMBEDDINGS_URL, which the chart sets when the
// embedding server is deployed and leaves unset when it is not.
func FromEnv() *Client {
	return NewClient(os.Getenv("EMBEDDINGS_URL"))
}

// Configured reports whether this installation has an embedding server.
//
// It takes a context and ignores it, satisfying agentmemory.Embedder — whose
// signature has one because the answer used to require a database read of an
// encrypted settings row. It is now a field, which is the point: an address
// supplied at startup cannot go stale between a sweep tick and a search the way
// a mutable setting could.
func (c *Client) Configured(context.Context) bool { return c.url != "" }

// Embed turns each text into a vector, in the same order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if !c.Configured(ctx) {
		return nil, ErrNotConfigured
	}
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(map[string]any{"input": texts})
	if err != nil {
		return nil, fmt.Errorf("embedding: encode request: %w", err)
	}

	var out struct {
		Vectors    [][]float32 `json:"vectors"`
		Model      string      `json:"model"`
		Dimensions int         `json:"dimensions"`
	}
	if err := c.post(ctx, "/embed", body, &out, requestTimeout); err != nil {
		return nil, err
	}

	// A short answer is a bug worth naming here rather than a misalignment to
	// discover later: the sweep pairs vectors with rows positionally, so a
	// response with fewer would silently attach one row's meaning to another's.
	if len(out.Vectors) != len(texts) {
		return nil, fmt.Errorf(
			"embedding: asked for %d vectors, got %d", len(texts), len(out.Vectors))
	}
	return out.Vectors, nil
}

// Status is what the embedding server says it is configured to do. It carries no
// credential — the server never returns one, and nothing here has one to leak.
type Status struct {
	// Configured is whether this installation has an embedding server at all.
	Configured bool `json:"configured"`
	// Reachable is whether it answered. False with Configured true is the
	// interesting case: deployed, and not responding.
	Reachable bool `json:"reachable"`
	// Model and Dimensions are what it reports about itself, so a mismatch with
	// the stored vector width is diagnosable from one request.
	Model      string `json:"model,omitempty"`
	Dimensions int    `json:"dimensions,omitempty"`
	// Detail is the transport error when it did not answer, or empty.
	Detail string `json:"detail,omitempty"`
}

// Check asks the embedding server what it is configured to do, without asking it
// to do any of it. Shallow on purpose: embedding something to find out whether
// the provider key is good would bill somebody every time a page refreshed.
func (c *Client) Check(ctx context.Context) Status {
	if !c.Configured(ctx) {
		return Status{}
	}
	var out struct {
		Model      string `json:"model"`
		Dimensions int    `json:"dimensions"`
	}
	if err := c.get(ctx, "/healthz", &out, statusTimeout); err != nil {
		return Status{Configured: true, Detail: err.Error()}
	}
	return Status{
		Configured: true, Reachable: true,
		Model: out.Model, Dimensions: out.Dimensions,
	}
}

// Probe adapts Check to the health package's probe signature: an error means
// unreachable.
func (c *Client) Probe(ctx context.Context) error {
	status := c.Check(ctx)
	if !status.Reachable {
		if status.Detail != "" {
			return fmt.Errorf("%s", status.Detail)
		}
		return ErrNotConfigured
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, body []byte, out any, timeout time.Duration) error {
	return c.do(ctx, http.MethodPost, path, body, out, timeout)
}

func (c *Client) get(ctx context.Context, path string, out any, timeout time.Duration) error {
	return c.do(ctx, http.MethodGet, path, nil, out, timeout)
}

func (c *Client) do(
	ctx context.Context, method, path string, body []byte, out any, timeout time.Duration,
) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url+path, reader)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBody))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		// The server's own message, verbatim and bounded. It is what carries the
		// provider's complaint — an unknown model, a rejected dimension count — and
		// paraphrasing it would put a second string between an operator and it.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return fmt.Errorf("embedding: %s: %s",
			resp.Status, strings.TrimSpace(string(detail)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("embedding: decode response: %w", err)
	}
	return nil
}
