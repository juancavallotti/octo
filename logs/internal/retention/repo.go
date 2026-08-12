package retention

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo persists the retention policy row. It owns the key and the marshalling;
// the row-level SQL lives in Store.
type Repo struct {
	store *Store
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{store: NewStore(pool)}
}

// Get returns the stored policy. An absent row is the zero value — keep
// everything — rather than an error.
func (r *Repo) Get(ctx context.Context) (stored, error) {
	raw, ok, err := r.store.Get(ctx, settingsKey)
	if err != nil || !ok {
		return stored{}, err
	}
	var s stored
	if err := json.Unmarshal(raw, &s); err != nil {
		return stored{}, fmt.Errorf("retention: decode stored value: %w", err)
	}
	return s, nil
}

// Put writes the policy, replacing whatever was there.
func (r *Repo) Put(ctx context.Context, s stored) error {
	value, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("retention: encode value: %w", err)
	}
	return r.store.Put(ctx, settingsKey, value)
}
