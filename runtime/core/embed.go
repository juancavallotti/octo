package core

import "context"

// EmbedClient is the provider-agnostic capability the ai-embed block depends on.
// Only llm-openai and llm-gemini implement it — Anthropic has no embeddings API —
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
}
