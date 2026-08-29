package core

import (
	"context"
	"errors"
	"testing"
)

// The no-op KV reads as an empty store rather than an error: a miss is the honest
// answer when nothing was ever written, and callers already handle it.
func TestNoopKVGetMisses(t *testing.T) {
	_, ok, err := NoopKV().Get(context.Background(), NamespaceUser, "k")
	if err != nil {
		t.Fatalf("Get err = %v, want nil", err)
	}
	if ok {
		t.Fatal("Get ok = true, want false: the no-op store holds nothing")
	}
}

// Writes fail loudly instead: silently dropping a value the caller believes was
// persisted is the failure mode ErrNoKV exists to prevent.
func TestNoopKVWritesReturnErrNoKV(t *testing.T) {
	kv := NoopKV()
	if _, err := kv.Set(context.Background(), NamespaceUser, "k", []byte("v"), 0); !errors.Is(err, ErrNoKV) {
		t.Fatalf("Set err = %v, want ErrNoKV", err)
	}
	if err := kv.Delete(context.Background(), NamespaceUser, "k", 0); !errors.Is(err, ErrNoKV) {
		t.Fatalf("Delete err = %v, want ErrNoKV", err)
	}
}
