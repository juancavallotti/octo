package agentmemory

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// The optional half of the store: turning stored text into vectors, and using
// them.
//
// Nothing here runs on a deployment that has not configured an embedding
// provider, and everything the store does still works when it does not. That is
// the shape the whole feature is built to: semantic search improves the ranking,
// it is not what makes search exist.

// Embedder produces vectors for text. The embedding package's Client satisfies
// it; it is an interface so this package does not depend on that one.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Configured reports whether an operator has set a provider up. It is checked
	// per call rather than at startup because it changes at runtime: an operator
	// turns this on, and off again, without restarting anything.
	Configured(ctx context.Context) bool
}

// Sweep pacing.
//
// The batch size and the interval together cap how fast a backfill spends
// somebody's money. A large install turning this on has a history of hundreds of
// thousands of rows, and a sweep with no pacing would send all of it to a paid API
// in one burst — which is a bill nobody agreed to and a rate limit nobody
// expected. At these numbers a hundred thousand rows takes a few hours, which is
// the right tempo for something an operator turns on and then stops thinking
// about.
const (
	sweepBatch    = 64
	sweepInterval = 5 * time.Second
	// idleInterval is how long the sweep waits when there is nothing to do. Longer,
	// because the steady state is nothing to do: new turns arrive as people talk,
	// not continuously.
	idleInterval = time.Minute
)

// WithEmbedder wires the optional vector half onto the service.
func (s *Service) WithEmbedder(e Embedder, sweeper Sweeper) *Service {
	s.embedder = e
	s.sweeper = sweeper
	return s
}

// Sweeper is the database half of the backfill: what has no vector, and where a
// vector goes. *Repo satisfies it.
type Sweeper interface {
	PendingEmbeddings(ctx context.Context, limit int) ([]Pending, error)
	SaveEmbeddings(ctx context.Context, rows []Pending, vectors [][]float32) error
	SearchVector(ctx context.Context, integrationID string, q Query, vector []float32) ([]Hit, error)
}

// StartBackfill runs the sweep until ctx is cancelled.
//
// It is best-effort throughout: a provider that is rate-limiting, a key that has
// been revoked, a model that answers with the wrong width — none of them should
// do anything but slow the sweep down and say so. The rows are still there, still
// searchable by text, and still pending when the configuration is fixed.
func (s *Service) StartBackfill(ctx context.Context) {
	if s.embedder == nil || s.sweeper == nil {
		return
	}
	go func() {
		timer := time.NewTimer(idleInterval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
			timer.Reset(s.sweepOnce(ctx))
		}
	}()
}

// sweepOnce embeds one batch and returns how long to wait before the next.
func (s *Service) sweepOnce(ctx context.Context) time.Duration {
	if !s.embedder.Configured(ctx) {
		return idleInterval
	}
	rows, err := s.sweeper.PendingEmbeddings(ctx, sweepBatch)
	if err != nil {
		slog.Warn("agent memory: could not read what needs embedding", "error", err)
		return idleInterval
	}
	if len(rows) == 0 {
		return idleInterval
	}
	texts := make([]string, 0, len(rows))
	for _, row := range rows {
		texts = append(texts, row.Text)
	}
	vectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		if ctx.Err() != nil {
			return idleInterval
		}
		// Backing right off rather than retrying immediately: the failures that
		// actually happen here are a rate limit and a bad credential, and hammering
		// makes the first worse and the second no better.
		slog.Warn("agent memory: embedding a batch failed; backing off", "rows", len(rows), "error", err)
		return idleInterval
	}
	if err := s.sweeper.SaveEmbeddings(ctx, rows, vectors); err != nil {
		slog.Warn("agent memory: could not store embeddings", "rows", len(rows), "error", err)
		return idleInterval
	}
	slog.Debug("agent memory: embedded a batch", "rows", len(rows))
	return sweepInterval
}

// searchSemantic ranks by embedding when one can be produced for the query.
//
// It returns ok=false rather than an error for every reason semantic search might
// not answer — not configured, provider unreachable, nothing embedded yet — because
// each of those means "use the text ranking", and none of them is worth failing a
// search over. A person looking for something they said last week does not care
// which index found it.
func (s *Service) searchSemantic(
	ctx context.Context, integrationID string, q Query,
) (hits []Hit, ok bool) {
	if s.embedder == nil || s.sweeper == nil || !s.embedder.Configured(ctx) {
		return nil, false
	}
	vectors, err := s.embedder.Embed(ctx, []string{q.Text})
	if err != nil || len(vectors) != 1 {
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("agent memory: could not embed the query; falling back to text search",
				"error", err)
		}
		return nil, false
	}
	hits, err = s.sweeper.SearchVector(ctx, integrationID, q, vectors[0])
	if err != nil {
		slog.Warn("agent memory: vector search failed; falling back to text search", "error", err)
		return nil, false
	}
	// Nothing embedded yet is the ordinary mid-backfill state, and it is
	// indistinguishable here from "nothing matches". Falling through to text costs
	// one more query and answers the question either way.
	if len(hits) == 0 {
		return nil, false
	}
	return hits, true
}
