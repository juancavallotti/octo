package standalone

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/core"
)

// ns is the namespace used by tests that exercise a single namespace.
const ns = "test"

func TestKVCreateAndGet(t *testing.T) {
	kv := newStore()
	ctx := context.Background()

	v, err := kv.Set(ctx, ns, "k", []byte("hello"), 0)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v != 1 {
		t.Fatalf("first version = %d, want 1", v)
	}

	entry, ok, err := kv.Get(ctx, ns, "k")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if string(entry.Value) != "hello" || entry.Version != 1 {
		t.Fatalf("Get = %q v%d, want \"hello\" v1", entry.Value, entry.Version)
	}
}

func TestKVGetMissing(t *testing.T) {
	kv := newStore()
	_, ok, err := kv.Get(context.Background(), ns, "absent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for an absent key")
	}
}

func TestKVCreateOverExistingConflicts(t *testing.T) {
	kv := newStore()
	ctx := context.Background()
	if _, err := kv.Set(ctx, ns, "k", []byte("a"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// expectedVersion 0 means create; the key now exists, so it must conflict.
	if _, err := kv.Set(ctx, ns, "k", []byte("b"), 0); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("create over existing: err = %v, want ErrVersionConflict", err)
	}
}

func TestKVOverwriteWithCurrentVersion(t *testing.T) {
	kv := newStore()
	ctx := context.Background()
	v1, _ := kv.Set(ctx, ns, "k", []byte("a"), 0)

	v2, err := kv.Set(ctx, ns, "k", []byte("b"), v1)
	if err != nil {
		t.Fatalf("Set with current version: %v", err)
	}
	if v2 != 2 {
		t.Fatalf("version after overwrite = %d, want 2", v2)
	}

	entry, _, _ := kv.Get(ctx, ns, "k")
	if string(entry.Value) != "b" || entry.Version != 2 {
		t.Fatalf("Get = %q v%d, want \"b\" v2", entry.Value, entry.Version)
	}
}

func TestKVStaleVersionConflicts(t *testing.T) {
	kv := newStore()
	ctx := context.Background()
	v1, _ := kv.Set(ctx, ns, "k", []byte("a"), 0)
	if _, err := kv.Set(ctx, ns, "k", []byte("b"), v1); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// v1 is now stale (current version is 2): the write must be rejected.
	if _, err := kv.Set(ctx, ns, "k", []byte("c"), v1); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("stale write: err = %v, want ErrVersionConflict", err)
	}
}

func TestKVNamespacesAreIsolated(t *testing.T) {
	kv := newStore()
	ctx := context.Background()

	// The same key in two namespaces is two independent entries.
	if _, err := kv.Set(ctx, core.NamespaceSystem, "k", []byte("secret-state"), 0); err != nil {
		t.Fatalf("Set system: %v", err)
	}
	// A write under the user namespace with expectedVersion 0 succeeds — it does not
	// see the system entry — and cannot read or clobber it.
	if _, err := kv.Set(ctx, core.NamespaceUser, "k", []byte("user-value"), 0); err != nil {
		t.Fatalf("Set user: %v", err)
	}

	if _, ok, _ := kv.Get(ctx, core.NamespaceUser, "k"); !ok {
		t.Fatal("user namespace key missing")
	}
	system, ok, _ := kv.Get(ctx, core.NamespaceSystem, "k")
	if !ok || string(system.Value) != "secret-state" {
		t.Fatalf("system entry was visible/clobbered across namespaces: %q ok=%v", system.Value, ok)
	}
}

func TestKVAndSecretsDoNotCollide(t *testing.T) {
	svc := New()
	ctx := context.Background()

	if _, err := svc.KV().Set(ctx, core.NamespaceUser, "k", []byte("kv-value"), 0); err != nil {
		t.Fatalf("KV Set: %v", err)
	}
	if _, err := svc.Secrets().Set(ctx, core.NamespaceUser, "k", []byte("secret-value"), 0); err != nil {
		t.Fatalf("Secrets Set: %v", err)
	}

	// The same key holds its own value in each store: the secret store routes to a
	// dedicated namespace, so they share the table without colliding.
	kvEntry, ok, _ := svc.KV().Get(ctx, core.NamespaceUser, "k")
	if !ok || string(kvEntry.Value) != "kv-value" {
		t.Fatalf("KV Get = %q ok=%v, want \"kv-value\"", kvEntry.Value, ok)
	}
	secretEntry, ok, _ := svc.Secrets().Get(ctx, core.NamespaceUser, "k")
	if !ok || string(secretEntry.Value) != "secret-value" {
		t.Fatalf("Secrets Get = %q ok=%v, want \"secret-value\"", secretEntry.Value, ok)
	}

	// The secret landed under the user_secrets namespace in the shared store.
	raw, ok, _ := svc.KV().Get(ctx, core.NamespaceUserSecrets, "k")
	if !ok || string(raw.Value) != "secret-value" {
		t.Fatalf("secret not stored under %q", core.NamespaceUserSecrets)
	}
}

func TestKVDelete(t *testing.T) {
	kv := newStore()
	ctx := context.Background()
	v1, _ := kv.Set(ctx, ns, "k", []byte("a"), 0)

	// Wrong version is rejected.
	if err := kv.Delete(ctx, ns, "k", v1+5); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Delete wrong version: err = %v, want ErrVersionConflict", err)
	}
	// Matching version succeeds.
	if err := kv.Delete(ctx, ns, "k", v1); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := kv.Get(ctx, ns, "k"); ok {
		t.Fatal("key still present after delete")
	}
	// Deleting an absent key is a no-op.
	if err := kv.Delete(ctx, ns, "k", 0); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
}

func TestKVUnconditionalDelete(t *testing.T) {
	kv := newStore()
	ctx := context.Background()
	_, _ = kv.Set(ctx, ns, "k", []byte("a"), 0)
	// expectedVersion 0 deletes regardless of the current version.
	if err := kv.Delete(ctx, ns, "k", 0); err != nil {
		t.Fatalf("unconditional Delete: %v", err)
	}
	if _, ok, _ := kv.Get(ctx, ns, "k"); ok {
		t.Fatal("key still present after unconditional delete")
	}
}

func TestGetReturnsCopy(t *testing.T) {
	kv := newStore()
	ctx := context.Background()
	_, _ = kv.Set(ctx, ns, "k", []byte("abc"), 0)

	entry, _, _ := kv.Get(ctx, ns, "k")
	entry.Value[0] = 'X' // mutate the returned copy

	again, _, _ := kv.Get(ctx, ns, "k")
	if string(again.Value) != "abc" {
		t.Fatalf("stored value mutated through returned slice: %q", again.Value)
	}
}

func TestLeaderElectionAlwaysLeader(t *testing.T) {
	svc := New()
	lease, err := svc.LeaderElection().Acquire(context.Background(), "any-key")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !lease.IsLeader() {
		t.Fatal("standalone leadership should always be the leader")
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
