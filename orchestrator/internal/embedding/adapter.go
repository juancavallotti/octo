package embedding

import (
	"context"
	"log/slog"
)

// Adapter is the embedding half of agent memory, as that package's Embedder.
//
// It exists so agentmemory depends on an interface it declares rather than on
// this package, and so the credential lookup happens per call: an operator turns
// embeddings on, off, and on again with a different key without restarting
// anything, and a client that read the settings once at startup would be
// answering with a key that has since been rotated away.
type Adapter struct {
	svc    *Service
	client *Client
}

// NewAdapter returns the adapter agentmemory's WithEmbedder takes.
func NewAdapter(svc *Service) *Adapter {
	return &Adapter{svc: svc, client: NewClient()}
}

// Configured reports whether an operator has set a provider up.
//
// A read of the settings row, on a path that runs once per sweep tick and once
// per search — cheap enough not to cache, and caching it would reintroduce
// exactly the staleness the adapter exists to avoid.
func (a *Adapter) Configured(ctx context.Context) bool {
	creds, err := a.svc.Reveal(ctx)
	if err != nil {
		// A key that will not decrypt is a real problem, but not one this can fix and
		// not one worth failing a search over. Reported, and treated as unconfigured.
		slog.Warn("embedding: could not read the configured credentials", "error", err)
		return false
	}
	return creds.Configured()
}

// Embed turns text into vectors with the currently configured provider.
func (a *Adapter) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	creds, err := a.svc.Reveal(ctx)
	if err != nil {
		return nil, err
	}
	return a.client.Embed(ctx, creds, texts)
}
