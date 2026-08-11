package sitesettings

import (
	"context"
	"encoding/json"
	"os"
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
