package core

import "context"

// EmbedClient is the provider-agnostic capability the ai-embed block depends on.
// Every provider connector but llm-anthropic implements it — Anthropic has no
// embeddings API —
// so a connector that doesn't satisfy this interface fails ai-embed's type
// assertion at flow-build time, the same way an unsupported provider fails any
// other capability-by-interface binding in this codebase.
//
// The interface lives in core (not a connector package) for the same reason
// LLMClient does: ai-embed resolves a connector by name through BlockDeps.Connector
// and type-asserts the result, so the interface has to sit somewhere both core and
// every connector package can reach without an import cycle.
//
// Implementations must be safe for concurrent use: one connector instance is
// shared across all flows that reference it.
type EmbedClient interface {
	// Embed turns each entry of req.Input into a vector, in the same order.
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
}

// EmbedRequest is a batch embedding call: every text in Input is embedded by the
// same model in one request.
type EmbedRequest struct {
	// Model is the provider-specific embedding model id (e.g.
	// text-embedding-3-small, gemini-embedding-001). Required.
	Model string
	// Input is one or more texts to embed, in order.
	Input []string
	// Dimensions requests a shortened output embedding when the model supports it
	// (OpenAI text-embedding-3-*, newer Gemini embedding models). Zero means the
	// model's default dimensionality.
	Dimensions int
}

// EmbedResponse is the result of one Embed call. Vectors has the same length and
// order as the request's Input.
type EmbedResponse struct {
	Vectors [][]float32
	// Usage is the call's token accounting, or nil when the provider reported none.
	// Nil is an ordinary outcome here rather than a sign of trouble: Gemini's
	// embeddings API returns no token count at all.
	Usage *EmbedUsage
	// Model is the model that actually served the call, as the provider reported
	// it, falling back to the id the request asked for when it reported none — the
	// same contract LLMResponse.Model carries, and for the same reason: it is the
	// model that answered that has a price.
	Model string
}

// EmbedUsage is the token accounting for one embedding call.
//
// There is one figure because there is one thing being billed. An embedding
// produces no output tokens, and OpenAI's total_tokens equals its prompt_tokens
// for every embeddings request, so carrying both would only invite a reader to add
// them together.
type EmbedUsage struct {
	InputTokens int
}
