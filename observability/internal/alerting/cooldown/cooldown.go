// Package cooldown remembers that a watch has already said something.
//
// One key per watch, set when an announcement goes out and left to expire. That
// is the whole mechanism, and Redis is the right place for it precisely because
// the record is disposable: losing it means somebody is told twice, which is the
// safe direction, where losing the alert state in Postgres would mean a hold
// restarting or an incident re-announcing itself.
//
// It is deliberately not where firing state lives. A hold is a count of
// consecutive checks that has to survive a restart, and how an episode ended —
// recovered, or simply undecidable — is a fact the history reads back. Neither
// is a thing to let expire.
package cooldown

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// keyPrefix namespaces the keys on a Redis shared with the trace folds and the
// volatile KV tier.
const keyPrefix = "octo:alerts:v0:cooldown:"

// Store records that a watch has announced something recently.
type Store struct {
	rdb *redis.Client
}

// New returns a store over rdb. A nil client is a store that suppresses nothing,
// which is what a process with no Redis has — and the honest failure, since the
// alternative is silently suppressing every alert.
func New(rdb *redis.Client) *Store {
	if rdb == nil {
		return nil
	}
	return &Store{rdb: rdb}
}

// Begin claims the cooldown for a watch.
//
// It reports whether the caller may announce: true when nothing held the key and
// it is now held for ttl, false when a previous announcement still holds it.
// SET NX is what makes that one round trip and atomic, so two evaluators racing
// — a lease that has just moved — cannot both decide they are first.
//
// A zero or negative ttl is "no cooldown configured" and always allows.
//
// A Redis failure allows. Suppression is the feature; being unable to check
// whether to suppress is not a reason to go quiet, and an alert delivered twice
// is recoverable in a way one never delivered is not.
func (s *Store) Begin(ctx context.Context, watchID string, ttl time.Duration) (bool, error) {
	if s == nil || ttl <= 0 {
		return true, nil
	}
	ok, err := s.rdb.SetNX(ctx, keyPrefix+watchID, time.Now().UTC().Format(time.RFC3339), ttl).Result()
	if err != nil {
		return true, fmt.Errorf("cooldown: claim for watch %s: %w", watchID, err)
	}
	return ok, nil
}

// Until reports when a watch's cooldown lifts, and whether one is in force. For
// the list view, so "why has this not told me anything" is answerable without
// reading the evaluation log.
func (s *Store) Until(ctx context.Context, watchID string) (time.Time, bool, error) {
	if s == nil {
		return time.Time{}, false, nil
	}
	ttl, err := s.rdb.TTL(ctx, keyPrefix+watchID).Result()
	if err != nil {
		return time.Time{}, false, fmt.Errorf("cooldown: read for watch %s: %w", watchID, err)
	}
	// Redis reports -2 for a key that is gone and -1 for one with no expiry.
	// Neither is a cooldown in force: this store only ever sets keys with a TTL,
	// so a key without one is not something it wrote.
	if ttl <= 0 {
		return time.Time{}, false, nil
	}
	return time.Now().UTC().Add(ttl), true, nil
}

// Clear drops a watch's cooldown, for somebody who has dealt with the thing and
// wants to be told again.
func (s *Store) Clear(ctx context.Context, watchID string) error {
	if s == nil {
		return nil
	}
	if err := s.rdb.Del(ctx, keyPrefix+watchID).Err(); err != nil {
		return fmt.Errorf("cooldown: clear for watch %s: %w", watchID, err)
	}
	return nil
}
