// Package servicestest holds the executable contract for the runtime service
// interfaces that have more than one implementation.
//
// Three modules implement core.KV and core.Leases — in a map, in an orchestrator
// database, and behind somebody else's HTTP API — and those two interfaces carry
// the subtlest rules in the whole surface. Version 0 creates and conflicts if the
// key is already there. A positive version must match. Deleting something absent
// succeeds. Acquire never blocks, and a claim whose renewal stops landing closes
// its Done channel. Each of those is a sentence that is easy to agree with and
// easy to implement differently, and three sets of hand-written tests is exactly
// the arrangement where one implementation quietly disagrees.
//
// It matters most for the api module, whose correctness depends on a third party
// implementing the contract we published. Writing the contract down as Go that
// runs is worth more there than it would have been with two in-tree modules.
//
// Deliberately scoped to these two. Queues and topics legitimately differ across
// modules — at-most-once in-process against at-least-once with acknowledgement —
// so a shared suite would have to be parameterized by capability before it could
// be honest, and one full of "if module ==" is worse than none. Agent memory has
// twelve methods and two of the three modules decline two of them. Those become
// suites when the real common subset is visible rather than guessed.
package servicestest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// staleOffset is added to a current version to make one that is definitely stale.
// Any positive number works; this one is far enough from a real version that a
// failure message is unmistakable.
const staleOffset = 99

// contractTimeout bounds a wait that should not happen: every operation in these
// suites either returns promptly or has violated the contract.
const contractTimeout = 5 * time.Second

// kvRule is one rule core.KV states, named as the sentence it checks.
type kvRule struct {
	name string
	run  func(t *testing.T, namespace string, kv core.KV)
}

// KVContract exercises every rule core.KV states. kv must be empty.
//
// namespace is passed in rather than fixed because a module may only accept the
// namespaces it was told about, and the caller knows which of theirs is safe to
// write to.
func KVContract(t *testing.T, namespace string, kv core.KV) {
	t.Helper()
	for _, rule := range kvRules() {
		t.Run(rule.name, func(t *testing.T) { rule.run(t, namespace, kv) })
	}
}

func kvRules() []kvRule {
	return []kvRule{
		{"read of an absent key is a miss, not an error", kvReadAbsent},
		{"version 0 creates and returns a positive version", kvCreate},
		{"a read returns the value and its version", kvReadBack},
		{"version 0 over an existing key conflicts", kvRecreateConflicts},
		{"a stale version conflicts and the current one succeeds", kvUpdate},
		{"deleting an absent key succeeds", kvDeleteAbsent},
		{"delete at a stale version conflicts", kvDeleteStale},
		{"delete removes the key", kvDelete},
		{"an empty value round-trips", kvEmptyValue},
	}
}

func kvReadAbsent(t *testing.T, namespace string, kv core.KV) {
	entry, ok, err := kv.Get(t.Context(), namespace, "contract/absent")
	if err != nil {
		t.Fatalf("Get err = %v, want nil", err)
	}
	if ok {
		t.Fatalf("Get ok = true for a key never written (value %q)", entry.Value)
	}
}

func kvCreate(t *testing.T, namespace string, kv core.KV) {
	v, err := kv.Set(t.Context(), namespace, "contract/create", []byte("one"), 0)
	if err != nil {
		t.Fatalf("Set err = %v", err)
	}
	if v <= 0 {
		t.Fatalf("Set returned version %d, want a positive one", v)
	}
}

func kvReadBack(t *testing.T, namespace string, kv core.KV) {
	want, err := kv.Set(t.Context(), namespace, "contract/read", []byte("value"), 0)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok, err := kv.Get(t.Context(), namespace, "contract/read")
	if err != nil || !ok {
		t.Fatalf("Get = (ok %v, err %v)", ok, err)
	}
	if string(entry.Value) != "value" {
		t.Fatalf("value = %q, want the bytes written", entry.Value)
	}
	if entry.Version != want {
		t.Fatalf("version = %d, want %d — the version a write returns is the "+
			"version the next read reports", entry.Version, want)
	}
}

func kvRecreateConflicts(t *testing.T, namespace string, kv core.KV) {
	key := "contract/recreate"
	if _, err := kv.Set(t.Context(), namespace, key, []byte("one"), 0); err != nil {
		t.Fatal(err)
	}
	_, err := kv.Set(t.Context(), namespace, key, []byte("two"), 0)
	if !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Set err = %v, want ErrVersionConflict: 0 means create, not overwrite", err)
	}
}

func kvUpdate(t *testing.T, namespace string, kv core.KV) {
	key := "contract/update"
	v, err := kv.Set(t.Context(), namespace, key, []byte("one"), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = kv.Set(t.Context(), namespace, key, []byte("two"), v+staleOffset)
	if !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Set at a stale version = %v, want ErrVersionConflict", err)
	}
	next, err := kv.Set(t.Context(), namespace, key, []byte("two"), v)
	if err != nil {
		t.Fatalf("Set at the current version = %v", err)
	}
	if next <= v {
		t.Fatalf("version went %d -> %d, want it to advance", v, next)
	}
}

func kvDeleteAbsent(t *testing.T, namespace string, kv core.KV) {
	if err := kv.Delete(t.Context(), namespace, "contract/never-existed", 0); err != nil {
		t.Fatalf("Delete err = %v, want nil: erasure of nothing is what the caller asked for", err)
	}
}

func kvDeleteStale(t *testing.T, namespace string, kv core.KV) {
	key := "contract/delete-stale"
	v, err := kv.Set(t.Context(), namespace, key, []byte("v"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Delete(t.Context(), namespace, key, v+staleOffset); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("Delete at a stale version = %v, want ErrVersionConflict", err)
	}
}

func kvDelete(t *testing.T, namespace string, kv core.KV) {
	key := "contract/delete"
	if _, err := kv.Set(t.Context(), namespace, key, []byte("v"), 0); err != nil {
		t.Fatal(err)
	}
	if err := kv.Delete(t.Context(), namespace, key, 0); err != nil {
		t.Fatalf("Delete err = %v", err)
	}
	if _, ok, _ := kv.Get(t.Context(), namespace, key); ok {
		t.Fatal("the key survived its delete")
	}
}

func kvEmptyValue(t *testing.T, namespace string, kv core.KV) {
	key := "contract/empty"
	if _, err := kv.Set(t.Context(), namespace, key, []byte{}, 0); err != nil {
		t.Fatalf("Set of an empty value = %v", err)
	}
	entry, ok, err := kv.Get(t.Context(), namespace, key)
	if err != nil || !ok {
		t.Fatalf("Get = (ok %v, err %v): an empty value is a value, not an absence", ok, err)
	}
	if len(entry.Value) != 0 {
		t.Fatalf("value = %q, want empty", entry.Value)
	}
}

// leaseRule is one rule core.Leases states.
type leaseRule struct {
	name string
	run  func(t *testing.T, leases core.Leases)
}

// LeasesContract exercises the rules core.Leases states.
//
// It cannot check take-over-after-expiry, which needs a module's own clock — each
// implementation tests that with the control it has. What it does check is the
// part every implementation must agree on: a claim is exclusive, Acquire never
// blocks, Close is idempotent, and losing the claim closes Done.
func LeasesContract(t *testing.T, leases core.Leases) {
	t.Helper()
	for _, rule := range leaseRules() {
		t.Run(rule.name, func(t *testing.T) { rule.run(t, leases) })
	}
}

func leaseRules() []leaseRule {
	return []leaseRule{
		{"a claim is exclusive and the loser is told immediately", leaseExclusive},
		{"a released name can be claimed again", leaseReleaseAndReclaim},
		{"Done closes when the claim is released", leaseDoneOnRelease},
		{"Close is idempotent", leaseCloseIdempotent},
		{"a cancelled context does not leave a claim held", leaseCancelledContext},
	}
}

func leaseExclusive(t *testing.T, leases core.Leases) {
	lease, ok, err := leases.Acquire(t.Context(), "contract-exclusive")
	if err != nil || !ok {
		t.Fatalf("first Acquire = (ok %v, err %v), want the claim", ok, err)
	}
	defer func() { _ = lease.Close() }()

	// The point of a fail-fast claim is that this returns rather than queues.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = leases.Acquire(t.Context(), "contract-exclusive")
	}()
	select {
	case <-done:
	case <-time.After(contractTimeout):
		t.Fatal("a second Acquire blocked; core.Leases never waits for a holder")
	}
}

func leaseReleaseAndReclaim(t *testing.T, leases core.Leases) {
	lease, ok, err := leases.Acquire(t.Context(), "contract-release")
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	again, ok, err := leases.Acquire(t.Context(), "contract-release")
	if err != nil || !ok {
		t.Fatalf("Acquire after release = (ok %v, err %v), want the claim", ok, err)
	}
	_ = again.Close()
}

func leaseDoneOnRelease(t *testing.T, leases core.Leases) {
	lease, ok, err := leases.Acquire(t.Context(), "contract-done")
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lease.Done():
	case <-time.After(contractTimeout):
		t.Fatal("Done did not close after Close; work under a lease gates on it")
	}
}

func leaseCloseIdempotent(t *testing.T, leases core.Leases) {
	lease, ok, err := leases.Acquire(t.Context(), "contract-idempotent")
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("first Close = %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil: a deferred Close beside an explicit "+
			"one must not be an error", err)
	}
}

func leaseCancelledContext(t *testing.T, leases core.Leases) {
	ctx, cancel := context.WithCancel(t.Context())
	lease, ok, err := leases.Acquire(ctx, "contract-cancel")
	if err != nil || !ok {
		t.Fatalf("Acquire = (ok %v, err %v)", ok, err)
	}
	cancel()
	if err := lease.Close(); err != nil {
		t.Fatalf("Close after the acquiring context was cancelled = %v", err)
	}
}
