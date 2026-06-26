package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// countingRegistry returns a registry whose "count" leaf increments counter and
// writes {"n": counter} into the body, so a test can tell whether the wrapped
// flow actually ran (a cache hit must not run it).
func countingRegistry(counter *int) *core.BlockRegistry {
	reg := core.NewBlockRegistry()
	reg.MustRegister("count", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			*counter++
			msg.Body = map[string]any{"n": float64(*counter)}
			return msg, nil
		}), nil
	})
	return reg
}

func buildCacheScope(t *testing.T, reg *core.BlockRegistry, key, ttl string) *cacheScope {
	t.Helper()
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "count"}}}
	proc, err := (&builder{reg: reg}).cacheScope(types.BlockConfig{
		Type: blockKindCacheScope,
		Key:  key,
		TTL:  ttl,
		Body: &body,
	})
	if err != nil {
		t.Fatalf("build cache-scope: %v", err)
	}
	cs, ok := proc.(*cacheScope)
	if !ok {
		t.Fatalf("cacheScope returned %T, want *cacheScope", proc)
	}
	return cs
}

func bodyN(t *testing.T, msg *types.Message) float64 {
	t.Helper()
	body, ok := msg.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want map", msg.Body)
	}
	n, ok := body["n"].(float64)
	if !ok {
		t.Fatalf("body.n is %T, want float64", body["n"])
	}
	return n
}

func TestCacheScopeStoresAndServes(t *testing.T) {
	count := 0
	cs := buildCacheScope(t, countingRegistry(&count), `"k"`, "1m")
	ctx, _ := withFakeServices(context.Background())

	first, err := cs.Process(ctx, mustMessage(t))
	if err != nil {
		t.Fatalf("first Process: %v", err)
	}
	if count != 1 || bodyN(t, first) != 1 {
		t.Fatalf("first run: count=%d body.n=%v, want 1/1", count, first.Body)
	}

	second, err := cs.Process(ctx, mustMessage(t))
	if err != nil {
		t.Fatalf("second Process: %v", err)
	}
	if count != 1 {
		t.Errorf("body ran again on a cache hit: count=%d, want 1", count)
	}
	if bodyN(t, second) != 1 {
		t.Errorf("cache hit body.n=%v, want the cached 1", second.Body)
	}
}

func TestCacheScopeKeyVaries(t *testing.T) {
	count := 0
	// Key off the body so two different inputs land under different cache keys.
	cs := buildCacheScope(t, countingRegistry(&count), "body.id", "")
	ctx, _ := withFakeServices(context.Background())

	a := mustMessage(t)
	a.Body = map[string]any{"id": "a"}
	b := mustMessage(t)
	b.Body = map[string]any{"id": "b"}

	if _, err := cs.Process(ctx, a); err != nil {
		t.Fatalf("Process a: %v", err)
	}
	if _, err := cs.Process(ctx, b); err != nil {
		t.Fatalf("Process b: %v", err)
	}
	if count != 2 {
		t.Errorf("distinct keys should each miss: count=%d, want 2", count)
	}
}

func TestCacheScopeExpiredEntryRecomputes(t *testing.T) {
	count := 0
	cs := buildCacheScope(t, countingRegistry(&count), `"k"`, "1m")
	ctx, kv := withFakeServices(context.Background())

	// Seed an already-expired envelope so the scope must treat it as a miss.
	expired := cacheEnvelope{
		ExpiresAt: time.Now().Add(-time.Hour).UnixNano(),
		Body:      json.RawMessage(`{"n":99}`),
	}
	raw, err := json.Marshal(expired)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if _, err = kv.Set(ctx, core.NamespaceUser, cacheKey("k"), raw, 0); err != nil {
		t.Fatalf("seed kv: %v", err)
	}

	out, err := cs.Process(ctx, mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if count != 1 {
		t.Errorf("expired entry should recompute: count=%d, want 1", count)
	}
	if bodyN(t, out) != 1 {
		t.Errorf("body.n=%v, want the recomputed 1 (not the stale 99)", out.Body)
	}
}

func TestCacheScopeTTLZeroNeverExpires(t *testing.T) {
	cs := buildCacheScope(t, countingRegistry(new(int)), `"k"`, "0")
	ctx, kv := withFakeServices(context.Background())

	if _, err := cs.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	entry, ok, err := kv.Get(ctx, core.NamespaceUser, cacheKey("k"))
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	var env cacheEnvelope
	if err := json.Unmarshal(entry.Value, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.ExpiresAt != 0 {
		t.Errorf("ttl 0 should store no expiry, got ExpiresAt=%d", env.ExpiresAt)
	}
}

func TestInvalidateCacheForcesRecompute(t *testing.T) {
	count := 0
	reg := countingRegistry(&count)
	cs := buildCacheScope(t, reg, `"k"`, "1m")
	ctx, _ := withFakeServices(context.Background())

	if _, err := cs.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("warm cache: %v", err)
	}
	if _, err := cs.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("hit cache: %v", err)
	}
	if count != 1 {
		t.Fatalf("setup: count=%d, want a warm cache at 1", count)
	}

	inv, err := newInvalidateCache(types.Settings{"key": `"k"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newInvalidateCache: %v", err)
	}
	if _, err = inv.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("invalidate Process: %v", err)
	}

	if _, err = cs.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("post-invalidate Process: %v", err)
	}
	if count != 2 {
		t.Errorf("invalidate should force a recompute: count=%d, want 2", count)
	}
}

func TestCacheScopeBuildValidation(t *testing.T) {
	reg := countingRegistry(new(int))
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "count"}}}
	tests := []struct {
		name string
		cfg  types.BlockConfig
	}{
		{name: "no body", cfg: types.BlockConfig{Type: blockKindCacheScope, Key: `"k"`}},
		{name: "no key", cfg: types.BlockConfig{Type: blockKindCacheScope, Body: &body}},
		{
			name: "bad key expr",
			cfg:  types.BlockConfig{Type: blockKindCacheScope, Key: "body.", Body: &body},
		},
		{
			name: "bad ttl",
			cfg:  types.BlockConfig{Type: blockKindCacheScope, Key: `"k"`, TTL: "soon", Body: &body},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&builder{reg: reg}).cacheScope(tt.cfg); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestInvalidateCacheBuildValidation(t *testing.T) {
	if _, err := newInvalidateCache(nil, core.BlockDeps{}); err == nil {
		t.Error("expected an error with no key")
	}
	if _, err := newInvalidateCache(types.Settings{"key": "body."}, core.BlockDeps{}); err == nil {
		t.Error("expected an error with a bad key expression")
	}
}
