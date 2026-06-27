package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func TestObjectWriteThenReadBody(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())

	writer, err := newObjectWrite(types.Settings{"key": `"order:" + body.id`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectWrite: %v", err)
	}
	reader, err := newObjectRead(types.Settings{"key": `"order:" + body.id`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectRead: %v", err)
	}

	msg := mustMessage(t)
	msg.Body = map[string]any{"id": "7", "amount": float64(50)}
	if _, err = writer.Process(ctx, msg); err != nil {
		t.Fatalf("write Process: %v", err)
	}

	// Read it back into a fresh message that only carries the key inputs.
	read := mustMessage(t)
	read.Body = map[string]any{"id": "7"}
	out, err := reader.Process(ctx, read)
	if err != nil {
		t.Fatalf("read Process: %v", err)
	}
	body, ok := out.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want map", out.Body)
	}
	if body["amount"] != float64(50) {
		t.Errorf("amount = %v, want 50", body["amount"])
	}
}

func TestObjectWriteValueExpression(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())

	writer, err := newObjectWrite(
		types.Settings{"key": `"k"`, "value": `{"doubled": body.n * 2.0}`},
		core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("newObjectWrite: %v", err)
	}
	reader, err := newObjectRead(types.Settings{"key": `"k"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectRead: %v", err)
	}

	msg := mustMessage(t)
	msg.Body = map[string]any{"n": float64(21)}
	if _, err = writer.Process(ctx, msg); err != nil {
		t.Fatalf("write Process: %v", err)
	}

	out, err := reader.Process(ctx, mustMessage(t))
	if err != nil {
		t.Fatalf("read Process: %v", err)
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["doubled"] != float64(42) {
		t.Errorf("body = %v, want {doubled:42}", out.Body)
	}
}

func TestObjectWriteOverwrites(t *testing.T) {
	ctx, kv := withFakeServices(context.Background())

	writer, err := newObjectWrite(types.Settings{"key": `"k"`, "value": "body.v"}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectWrite: %v", err)
	}

	for _, v := range []float64{1, 2, 3} {
		msg := mustMessage(t)
		msg.Body = map[string]any{"v": v}
		if _, err = writer.Process(ctx, msg); err != nil {
			t.Fatalf("write Process: %v", err)
		}
	}

	entry, ok, err := kv.Get(ctx, core.NamespaceUser, "k")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if entry.Version != 3 {
		t.Errorf("version = %d, want 3 after three writes", entry.Version)
	}
	if string(entry.Value) != "3" {
		t.Errorf("value = %s, want 3", entry.Value)
	}
}

func TestObjectReadIntoVariable(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())

	writer, err := newObjectWrite(types.Settings{"key": `"k"`, "value": `{"hits": 5}`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectWrite: %v", err)
	}
	reader, err := newObjectRead(types.Settings{"key": `"k"`, "as": "stored"}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectRead: %v", err)
	}

	if _, err = writer.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("write Process: %v", err)
	}

	msg := mustMessage(t)
	msg.Body = "original"
	out, err := reader.Process(ctx, msg)
	if err != nil {
		t.Fatalf("read Process: %v", err)
	}
	if out.Body != "original" {
		t.Errorf("body = %v, want it untouched in as-mode", out.Body)
	}
	stored, ok := out.Variables["stored"].(map[string]any)
	if !ok || stored["hits"] != float64(5) {
		t.Errorf("vars.stored = %v, want {hits:5}", out.Variables["stored"])
	}
}

func TestObjectReadMissingKey(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())

	t.Run("body mode nulls the body", func(t *testing.T) {
		reader, err := newObjectRead(types.Settings{"key": `"absent"`}, core.BlockDeps{})
		if err != nil {
			t.Fatalf("newObjectRead: %v", err)
		}
		msg := mustMessage(t)
		msg.Body = "stale"
		out, err := reader.Process(ctx, msg)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if out.Body != nil {
			t.Errorf("body = %v, want nil on a miss", out.Body)
		}
	})

	t.Run("as mode leaves the variable unset", func(t *testing.T) {
		reader, err := newObjectRead(types.Settings{"key": `"absent"`, "as": "x"}, core.BlockDeps{})
		if err != nil {
			t.Fatalf("newObjectRead: %v", err)
		}
		out, err := reader.Process(ctx, mustMessage(t))
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		if _, ok := out.Variables["x"]; ok {
			t.Error("vars.x should be unset on a miss")
		}
	})
}

func TestObjectDeleteRemovesKey(t *testing.T) {
	ctx, kv := withFakeServices(context.Background())

	writer, err := newObjectWrite(types.Settings{"key": `"order:" + body.id`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectWrite: %v", err)
	}
	deleter, err := newObjectDelete(types.Settings{"key": `"order:" + body.id`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectDelete: %v", err)
	}

	msg := mustMessage(t)
	msg.Body = map[string]any{"id": "7"}
	if _, err = writer.Process(ctx, msg); err != nil {
		t.Fatalf("write Process: %v", err)
	}

	del := mustMessage(t)
	del.Body = map[string]any{"id": "7"}
	out, err := deleter.Process(ctx, del)
	if err != nil {
		t.Fatalf("delete Process: %v", err)
	}
	if out != del {
		t.Error("delete should pass the message through unchanged")
	}

	if _, ok, getErr := kv.Get(ctx, core.NamespaceUser, "order:7"); getErr != nil || ok {
		t.Errorf("Get after delete: ok=%v err=%v, want the key gone", ok, getErr)
	}
}

func TestObjectDeleteMissingKeyIsNoop(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())

	deleter, err := newObjectDelete(types.Settings{"key": `"absent"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectDelete: %v", err)
	}
	if _, err := deleter.Process(ctx, mustMessage(t)); err != nil {
		t.Errorf("deleting a missing key should be a no-op, got: %v", err)
	}
}

func TestObjectWriteFailsWithoutStore(t *testing.T) {
	// No services on the context: the noop KV rejects writes, and the block
	// surfaces that rather than silently dropping the value.
	writer, err := newObjectWrite(types.Settings{"key": `"k"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newObjectWrite: %v", err)
	}
	if _, err := writer.Process(context.Background(), mustMessage(t)); err == nil {
		t.Error("expected an error writing with no store configured")
	}
}

func TestObjectBuildValidation(t *testing.T) {
	tests := []struct {
		name    string
		factory core.BlockFactory
		raw     types.Settings
	}{
		{name: "object-read without key", factory: newObjectRead, raw: nil},
		{name: "object-read bad key expr", factory: newObjectRead, raw: types.Settings{"key": "body."}},
		{name: "object-write without key", factory: newObjectWrite, raw: nil},
		{name: "object-write bad key expr", factory: newObjectWrite, raw: types.Settings{"key": "body."}},
		{name: "object-write bad value expr", factory: newObjectWrite, raw: types.Settings{"key": `"k"`, "value": "body."}},
		{name: "object-delete without key", factory: newObjectDelete, raw: nil},
		{name: "object-delete bad key expr", factory: newObjectDelete, raw: types.Settings{"key": "body."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.factory(tt.raw, core.BlockDeps{}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
