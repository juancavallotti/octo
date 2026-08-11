package sitesettings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestStore opens a pool against TEST_DATABASE_URL, skipping the test when it is
// unset so `go test ./...` stays green without a database. Each test uses a random
// key so rows never collide with the real settings (or with each other).
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run sitesettings store tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	key := "test_" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM site_settings WHERE key = $1`, key)
		pool.Close()
	})
	return NewStore(pool), key
}

func TestStoreGetAbsentKey(t *testing.T) {
	store, key := newTestStore(t)

	value, ok, err := store.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if ok {
		t.Fatal("absent key reported as present")
	}
	if value != nil {
		t.Fatalf("value = %s, want nil", value)
	}
}

func TestStorePutGetRoundTrip(t *testing.T) {
	store, key := newTestStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, key, json.RawMessage(`{"provider":"resend","n":1}`)); err != nil {
		t.Fatalf("put: %v", err)
	}

	value, ok, err := store.Get(ctx, key)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	var got struct {
		Provider string `json:"provider"`
		N        int    `json:"n"`
	}
	if err := json.Unmarshal(value, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Provider != "resend" || got.N != 1 {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestStoreMutateReportsWhetherTheRowExisted(t *testing.T) {
	store, key := newTestStore(t)
	ctx := context.Background()

	var sawOK bool
	if err := store.Mutate(ctx, key, func(_ json.RawMessage, ok bool) (json.RawMessage, error) {
		sawOK = ok
		return json.RawMessage(`{"n":1}`), nil
	}); err != nil {
		t.Fatalf("first mutate: %v", err)
	}
	if sawOK {
		t.Fatal("ok = true on a key that did not exist")
	}

	var seen json.RawMessage
	if err := store.Mutate(ctx, key, func(current json.RawMessage, ok bool) (json.RawMessage, error) {
		sawOK, seen = ok, current
		return json.RawMessage(`{"n":2}`), nil
	}); err != nil {
		t.Fatalf("second mutate: %v", err)
	}
	if !sawOK {
		t.Fatal("ok = false on a key that existed")
	}
	// Compared by value rather than by bytes: jsonb is parsed on the way in, so what
	// comes back is Postgres's rendering of the value, not the text that was written.
	if n := decodeN(t, seen); n != 1 {
		t.Fatalf("current n = %d, want 1 (the first value)", n)
	}
}

// decodeN pulls the "n" field out of a stored value.
func decodeN(t *testing.T, raw json.RawMessage) int {
	t.Helper()
	var v struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return v.N
}

// A rejected change must leave the stored value untouched, and must surface the
// caller's own error rather than a wrapped database one.
func TestStoreMutateRollsBackOnError(t *testing.T) {
	store, key := newTestStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, key, json.RawMessage(`{"n":1}`)); err != nil {
		t.Fatalf("put: %v", err)
	}

	sentinel := errors.New("caller rejected the change")
	err := store.Mutate(ctx, key, func(json.RawMessage, bool) (json.RawMessage, error) {
		return json.RawMessage(`{"n":99}`), sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the caller's sentinel unwrapped", err)
	}

	value, _, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if n := decodeN(t, value); n != 1 {
		t.Fatalf("n = %d, want 1 — the rejected change must not be written", n)
	}
}

// Two concurrent read-modify-writes must both be reflected. With a plain Get/Put
// pair the second read happens before the first write and one increment is lost —
// which, for the real callers, is a silently discarded API key rotation.
func TestStoreMutateSerializesConcurrentWriters(t *testing.T) {
	store, key := newTestStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, key, json.RawMessage(`{"n":0}`)); err != nil {
		t.Fatalf("put: %v", err)
	}

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Mutate(ctx, key, func(current json.RawMessage, _ bool) (json.RawMessage, error) {
				var v struct {
					N int `json:"n"`
				}
				if err := json.Unmarshal(current, &v); err != nil {
					return nil, err
				}
				v.N++
				return json.Marshal(v)
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("mutate: %v", err)
		}
	}

	value, _, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal(value, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.N != writers {
		t.Fatalf("n = %d, want %d — an update was lost", got.N, writers)
	}
}

func TestStorePutOverwrites(t *testing.T) {
	store, key := newTestStore(t)
	ctx := context.Background()

	if err := store.Put(ctx, key, json.RawMessage(`{"n":1}`)); err != nil {
		t.Fatalf("first put: %v", err)
	}
	if err := store.Put(ctx, key, json.RawMessage(`{"n":2}`)); err != nil {
		t.Fatalf("second put: %v", err)
	}

	value, ok, err := store.Get(ctx, key)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	var got struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal(value, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.N != 2 {
		t.Fatalf("n = %d, want 2 (the second put should win)", got.N)
	}
}
