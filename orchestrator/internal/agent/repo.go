package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// Repo persists what the orchestrator knows about the agent it installed. It owns
// the key and the marshalling; the row-level SQL lives in sitesettings.Store.
type Repo struct {
	store *sitesettings.Store
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{store: sitesettings.NewStore(pool)}
}

// Get returns the stored record. An absent row is the zero value, not an error —
// "never installed" is a state, not a failure.
func (r *Repo) Get(ctx context.Context) (stored, error) {
	raw, ok, err := r.store.Get(ctx, settingsKey)
	if err != nil || !ok {
		return stored{}, err
	}
	var s stored
	if err := json.Unmarshal(raw, &s); err != nil {
		return stored{}, fmt.Errorf("agent settings: decode stored value: %w", err)
	}
	return s, nil
}

// Mutate applies fn to the stored record and writes the result back, with the row
// locked for the duration.
//
// The lock is the point. An install reads "is there an integration?" and then
// creates one; two installs racing without it create two integrations, of which the
// second fails on the unique name and leaves the first orphaned in the settings.
// Holding the row for the whole operation makes install idempotent under
// concurrency rather than only under sequence.
func (r *Repo) Mutate(ctx context.Context, fn func(current stored) (stored, error)) error {
	return r.store.Mutate(ctx, settingsKey, func(raw json.RawMessage, ok bool) (json.RawMessage, error) {
		var cur stored
		if ok {
			if err := json.Unmarshal(raw, &cur); err != nil {
				return nil, fmt.Errorf("agent settings: decode stored value: %w", err)
			}
		}
		next, err := fn(cur)
		if err != nil {
			return nil, err
		}
		out, err := json.Marshal(next)
		if err != nil {
			return nil, fmt.Errorf("agent settings: encode value: %w", err)
		}
		return out, nil
	})
}
