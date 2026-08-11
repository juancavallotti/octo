package email

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// Repo persists the email settings row. It owns the key and the marshalling; the
// row-level SQL lives in sitesettings.Store.
type Repo struct {
	store *sitesettings.Store
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{store: sitesettings.NewStore(pool)}
}

// Get returns the stored settings. An absent row is the zero value, not an error:
// "email has never been configured" is a state the caller handles, not a failure.
func (r *Repo) Get(ctx context.Context) (stored, error) {
	raw, ok, err := r.store.Get(ctx, settingsKey)
	if err != nil || !ok {
		return stored{}, err
	}
	var s stored
	if err := json.Unmarshal(raw, &s); err != nil {
		return stored{}, fmt.Errorf("email settings: decode stored value: %w", err)
	}
	return s, nil
}

// Put replaces the stored settings.
func (r *Repo) Put(ctx context.Context, s stored) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("email settings: encode value: %w", err)
	}
	return r.store.Put(ctx, settingsKey, raw)
}
