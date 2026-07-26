package types

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// containerIdentity reports which container a value points at, so a test can tell
// "the same map" from "an equal map".
func containerIdentity(t *testing.T, value any) uintptr {
	t.Helper()
	v := reflect.ValueOf(value)
	if kind := v.Kind(); kind != reflect.Map && kind != reflect.Slice {
		t.Fatalf("value of kind %s has no container identity to compare", kind)
	}
	return v.Pointer()
}

func TestCopyBody(t *testing.T) {
	t.Run("nested containers are independent", func(t *testing.T) {
		original := map[string]any{
			"outer": map[string]any{"inner": "before"},
			"list":  []any{map[string]any{"n": 1.0}},
		}

		out := copyBody(original).(map[string]any)
		out["outer"].(map[string]any)["inner"] = "after"
		out["list"].([]any)[0].(map[string]any)["n"] = 2.0

		if got := original["outer"].(map[string]any)["inner"]; got != "before" {
			t.Errorf("mutating a nested map in the copy changed the original: %v", got)
		}
		if got := original["list"].([]any)[0].(map[string]any)["n"]; got != 1.0 {
			t.Errorf("mutating a map inside a slice in the copy changed the original: %v", got)
		}
	})

	t.Run("containers are new storage", func(t *testing.T) {
		original := map[string]any{"list": []any{1.0}}

		out := copyBody(original).(map[string]any)

		if containerIdentity(t, out) == containerIdentity(t, original) {
			t.Error("the copy points at the original map")
		}
		if containerIdentity(t, out["list"]) == containerIdentity(t, original["list"]) {
			t.Error("the copy points at the original slice")
		}
	})

	t.Run("the result round-trips identically to the original", func(t *testing.T) {
		var original any
		raw := []byte(`{"s":"v","n":1.5,"b":true,"null":null,"list":[1,{"k":"v"}],"obj":{"deep":[]}}`)
		if err := json.Unmarshal(raw, &original); err != nil {
			t.Fatalf("Unmarshal returned error: %v", err)
		}

		copied := copyBody(original)
		if !reflect.DeepEqual(copied, original) {
			t.Errorf("copy = %#v, want %#v", copied, original)
		}
	})

	// The structural walk skips serialization, so values off the JSON-kinds
	// contract must still reach the round-trip that normalizes them — otherwise a
	// body would silently keep kinds that CEL cannot resolve.
	t.Run("off-contract values are normalized as before", func(t *testing.T) {
		type point struct {
			X int `json:"x"`
		}
		original := map[string]any{"n": 3, "p": point{X: 4}}

		out := copyBody(original).(map[string]any)

		if got, want := out["n"], 3.0; got != want {
			t.Errorf("int normalized to %#v, want %#v", got, want)
		}
		got, isMap := out["p"].(map[string]any)
		if !isMap {
			t.Fatalf("struct normalized to %T, want map[string]any", out["p"])
		}
		if got["x"] != 4.0 {
			t.Errorf("struct field = %#v, want 4.0", got["x"])
		}
	})

	// A value that will not encode is handed back rather than dropped, so the
	// copyable part of the body survives and the rest stays shared.
	t.Run("an uncopyable leaf is kept, not dropped", func(t *testing.T) {
		bad := make(chan int)
		original := map[string]any{"ok": "v", "bad": bad}

		out := copyBody(original).(map[string]any)

		if out["ok"] != "v" {
			t.Errorf("the copyable part was lost: %#v", out["ok"])
		}
		if out["bad"] != any(bad) {
			t.Errorf("the uncopyable leaf = %#v, want it handed back as-is", out["bad"])
		}
		if containerIdentity(t, out) == containerIdentity(t, original) {
			t.Error("the surrounding map was shared instead of copied")
		}
	})

	// A cycle is off-contract — no decode produces one — but a block can build one,
	// and following it would overflow the stack, which no recover can catch. The
	// depth cap turns that into the same graceful sharing an uncopyable leaf gets.
	t.Run("a cyclic body is shared, not chased", func(t *testing.T) {
		cyclic := map[string]any{"ok": "v"}
		cyclic["self"] = cyclic

		copied := copyBody(cyclic)

		out, isMap := copied.(map[string]any)
		if !isMap {
			t.Fatalf("copyBody returned %T, want map[string]any", copied)
		}
		if out["ok"] != "v" {
			t.Errorf("the copyable part was lost: %#v", out["ok"])
		}
	})

	t.Run("scalars and nil are handed straight back", func(t *testing.T) {
		for _, value := range []any{nil, "s", 1.5, true} {
			if got := copyBody(value); !reflect.DeepEqual(got, value) {
				t.Errorf("copyBody(%#v) = %#v, want the value back", value, got)
			}
		}
	})

	// A nil container marshals to null and decodes back to an untyped nil; the
	// structural path has to agree rather than inventing an empty container.
	t.Run("nil containers become untyped nil", func(t *testing.T) {
		for name, value := range map[string]any{
			"map":   map[string]any(nil),
			"slice": []any(nil),
		} {
			if got := copyBody(value); got != nil {
				t.Errorf("copyBody(nil %s) = %#v, want nil", name, got)
			}
		}
	})
}

// bodySink holds each benchmarked copy so the call cannot be optimized away.
var bodySink any

// benchBody builds a records-shaped body of n elements — the shape a foreach map
// or a fork branch actually copies.
func benchBody(n int) any {
	records := make([]any, n)
	for i := range records {
		records[i] = map[string]any{
			"id":     fmt.Sprintf("rec-%d", i),
			"amount": float64(i) * 1.5,
			"active": i%2 == 0,
			"nested": map[string]any{"tags": []any{"a", "b"}},
		}
	}
	return map[string]any{"records": records}
}

func BenchmarkCopyBody(b *testing.B) {
	for _, n := range []int{100, 400, 1600} {
		body := benchBody(n)
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				bodySink = copyBody(body)
			}
		})
	}
}

// BenchmarkJSONCopy measures the off-contract fallback, which is also the
// implementation copyBody replaced. It is the baseline the structural walk is
// worth reading against: same shape, same sizes, both linear.
func BenchmarkJSONCopy(b *testing.B) {
	for _, n := range []int{100, 400, 1600} {
		body := benchBody(n)
		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				bodySink = jsonCopy(body)
			}
		})
	}
}
