package websearch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// Repo persists the web search settings row. It owns the key and the marshalling;
// the row-level SQL lives in sitesettings.Store.
type Repo struct {
	store *sitesettings.Store
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{store: sitesettings.NewStore(pool)}
}

// Get returns the stored settings. An absent row is the zero value, not an error.
func (r *Repo) Get(ctx context.Context) (stored, error) {
	raw, ok, err := r.store.Get(ctx, settingsKey)
	if err != nil || !ok {
		return stored{}, err
	}
	var s stored
	if err := json.Unmarshal(raw, &s); err != nil {
		return stored{}, fmt.Errorf("web search settings: decode stored value: %w", err)
	}
	return s, nil
}

// Mutate applies fn to the stored settings and writes the result back, with the row
// locked for the duration — the same single write path the LLM settings use, so a
// save that carries the existing key forward cannot race a rotation.
func (r *Repo) Mutate(ctx context.Context, fn func(current stored) (stored, error)) error {
	return r.store.Mutate(ctx, settingsKey, func(raw json.RawMessage, ok bool) (json.RawMessage, error) {
		var cur stored
		if ok {
			if err := json.Unmarshal(raw, &cur); err != nil {
				return nil, fmt.Errorf("web search settings: decode stored value: %w", err)
			}
		}
		next, err := fn(cur)
		if err != nil {
			return nil, err
		}
		out, err := json.Marshal(next)
		if err != nil {
			return nil, fmt.Errorf("web search settings: encode value: %w", err)
		}
		return out, nil
	})
}
