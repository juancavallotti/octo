package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client turns text into vectors.
//
// It speaks two shapes, not three: OpenAI's, which OpenRouter also serves, and
// Google's. That is the whole surface — the orchestrator needs exactly one
// operation from these providers, and a general LLM client for it would be a
// second implementation of what the runtime's connectors already are, in a module
// that cannot import them.
type Client struct {
	http *http.Client
}

// requestTimeout bounds one call to a provider. Embedding a batch is a single
// round trip and a fast one; a minute is generous.
const requestTimeout = 60 * time.Second

// NewClient returns an embedding client.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: requestTimeout}}
}

// Embed returns one vector per input, in order.
//
// It refuses vectors of the wrong width rather than storing them. A column is
// vector(1536), so a model that answers 768 does not fail on write — it fails on
// query, later, with nothing pointing at the model as the cause.
func (c *Client) Embed(ctx context.Context, creds Credentials, texts []string) ([][]float32, error) {
	if !creds.Configured() {
		return nil, ErrNotConfigured
	}
	if len(texts) == 0 {
		return nil, nil
	}
	var (
		vectors [][]float32
		err     error
	)
	switch creds.Provider {
	case ProviderGoogle:
		vectors, err = c.embedGoogle(ctx, creds, texts)
	default:
		vectors, err = c.embedOpenAI(ctx, creds, texts)
	}
	if err != nil {
		return nil, err
	}
	for _, v := range vectors {
		if len(v) != Dimensions {
			return nil, fmt.Errorf("%w: %s produced %d dimensions, the store holds %d",
				ErrWrongDimensions, creds.Model, len(v), Dimensions)
		}
	}
	return vectors, nil
}

// openAIBase and openRouterBase are the two hosts that speak the OpenAI shape.
const (
	openAIBase     = "https://api.openai.com/v1"
	openRouterBase = "https://openrouter.ai/api/v1"
	googleBase     = "https://generativelanguage.googleapis.com/v1beta"
)

// embedOpenAI calls the OpenAI-shaped /embeddings endpoint.
//
// dimensions is always sent. text-embedding-3-small is natively 1536 and ignores
// it; text-embedding-3-large is 3072 and needs it. Sending it unconditionally is
// what lets an operator choose either without the platform keeping a table of
// which models need what.
func (c *Client) embedOpenAI(ctx context.Context, creds Credentials, texts []string) ([][]float32, error) {
	base := openAIBase
	if creds.Provider == ProviderOpenRouter {
		base = openRouterBase
	}
	body, err := json.Marshal(map[string]any{
		"model": creds.Model, "input": texts, "dimensions": Dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("embedding: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.APIKey)

	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, err
	}
	// Ordered by the index the provider reports, not by arrival: the response is
	// documented as unordered, and a mismatch here would attach every vector to the
	// wrong turn — which produces search results that are confidently wrong rather
	// than absent.
	vectors := make([][]float32, len(texts))
	for _, item := range out.Data {
		if item.Index < 0 || item.Index >= len(vectors) {
			return nil, fmt.Errorf("embedding: provider returned index %d for %d inputs",
				item.Index, len(texts))
		}
		vectors[item.Index] = item.Embedding
	}
	for i, v := range vectors {
		if v == nil {
			return nil, fmt.Errorf("embedding: provider returned no vector for input %d", i)
		}
	}
	return vectors, nil
}

// embedGoogle calls the Gemini batchEmbedContents endpoint.
func (c *Client) embedGoogle(ctx context.Context, creds Credentials, texts []string) ([][]float32, error) {
	model := creds.Model
	if !strings.HasPrefix(model, "models/") {
		model = "models/" + model
	}
	requests := make([]map[string]any, 0, len(texts))
	for _, text := range texts {
		requests = append(requests, map[string]any{
			"model":                model,
			"content":              map[string]any{"parts": []map[string]string{{"text": text}}},
			"outputDimensionality": Dimensions,
		})
	}
	body, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		return nil, fmt.Errorf("embedding: encode request: %w", err)
	}
	endpoint := fmt.Sprintf("%s/%s:batchEmbedContents", googleBase, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embedding: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", creds.APIKey)

	var out struct {
		Embeddings []struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	}
	if err := c.send(req, &out); err != nil {
		return nil, err
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embedding: provider returned %d vectors for %d inputs",
			len(out.Embeddings), len(texts))
	}
	vectors := make([][]float32, 0, len(texts))
	for _, e := range out.Embeddings {
		vectors = append(vectors, e.Values)
	}
	return vectors, nil
}

// errorBodySnippet caps how much of a failure body is quoted back.
const errorBodySnippet = 512

// send issues a request and decodes a successful response.
//
// The error carries a snippet of the body because the useful part of an
// embeddings failure is always in it — a wrong model name, a key without the
// right scope — and a bare status code sends an operator to the provider's
// dashboard to find out what this already knows.
func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("embedding: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodySnippet))
		msg := strings.TrimSpace(string(snippet))
		if msg == "" {
			return fmt.Errorf("embedding: provider returned %s", resp.Status)
		}
		return fmt.Errorf("embedding: provider returned %s: %s", resp.Status, msg)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("embedding: decode response: %w", err)
	}
	return nil
}
