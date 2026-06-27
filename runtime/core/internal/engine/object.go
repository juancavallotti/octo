// Built-in leaf blocks that read and write objects in the runtime KV store:
// object-read and object-write. They register on the process-wide block registry
// so they are always available once the engine is linked, and reach the store
// through the runtime services carried on the context
// (core.RuntimeServicesFromContext), so no connector is required.
//
// Both confine themselves to the user namespace (core.NamespaceUser): the store
// isolates keys per namespace, so a user flow's objects never collide with or
// expose internal runtime state. Each block compiles its CEL expressions once at
// build time, so a malformed expression fails at startup rather than per message.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

// objectWriteAttempts bounds the optimistic-concurrency retry loop of an
// object-write: a write re-reads the current version and retries on a version
// conflict, so a concurrent writer cannot make it spin forever.
const objectWriteAttempts = 5

func init() {
	core.MustRegisterBlock("object-read", newObjectRead)
	core.MustRegisterBlock("object-write", newObjectWrite)
	core.MustRegisterBlock("object-delete", newObjectDelete)
}

// objectWriteSettings configures the object-write block.
type objectWriteSettings struct {
	// Key is a CEL expression evaluated to the object key (required).
	Key string `json:"key"`
	// Value is a CEL expression whose result is stored. When empty the whole
	// message body is stored.
	Value string `json:"value"`
}

// objectWrite stores a value in the user KV namespace under an evaluated key.
type objectWrite struct {
	key   *expr.Program
	value *expr.Program // nil stores the message body
	env   map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newObjectWrite(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg objectWriteSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Key == "" {
		return nil, errors.New("object-write requires a key expression")
	}
	key, err := expr.Compile(cfg.Key, exprVarNames...)
	if err != nil {
		return nil, err
	}

	block := &objectWrite{key: key, env: envActivation(deps.Env)}
	if cfg.Value != "" {
		value, valueErr := expr.Compile(cfg.Value, exprVarNames...)
		if valueErr != nil {
			return nil, valueErr
		}
		block.value = value
	}
	return block, nil
}

// Process evaluates the key and value, encodes the value, and stores it under the
// key using optimistic concurrency (re-reading the version and retrying on a
// conflict). The message passes through unchanged.
func (p *objectWrite) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := messageActivation(msg, p.env)
	key, err := p.key.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("object-write key: %w", err)
	}

	value := msg.Body
	if p.value != nil {
		value, err = p.value.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("object-write value: %w", err)
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("object-write %q: encode value: %w", key, err)
	}

	kv := core.RuntimeServicesFromContext(ctx).KV()
	for attempt := 0; attempt < objectWriteAttempts; attempt++ {
		entry, _, getErr := kv.Get(ctx, core.NamespaceUser, key)
		if getErr != nil {
			return nil, fmt.Errorf("object-write %q: read version: %w", key, getErr)
		}
		if _, setErr := kv.Set(ctx, core.NamespaceUser, key, encoded, entry.Version); setErr != nil {
			if errors.Is(setErr, core.ErrVersionConflict) {
				continue // a concurrent writer won; re-read and retry
			}
			return nil, fmt.Errorf("object-write %q: %w", key, setErr)
		}
		return msg, nil
	}
	return nil, fmt.Errorf("object-write %q: %w after %d attempts", key, core.ErrVersionConflict, objectWriteAttempts)
}

// objectReadSettings configures the object-read block.
type objectReadSettings struct {
	// Key is a CEL expression evaluated to the object key (required).
	Key string `json:"key"`
	// As names a variable to store the object under (readable later as
	// vars.<As>). When empty the object replaces the message body.
	As string `json:"as"`
}

// objectRead reads an object from the user KV namespace into the message body or
// a named variable.
type objectRead struct {
	key *expr.Program
	as  string // empty folds the object into the body
	env map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newObjectRead(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg objectReadSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Key == "" {
		return nil, errors.New("object-read requires a key expression")
	}
	key, err := expr.Compile(cfg.Key, exprVarNames...)
	if err != nil {
		return nil, err
	}
	return &objectRead{key: key, as: cfg.As, env: envActivation(deps.Env)}, nil
}

// Process evaluates the key, reads the object, and folds it into the body (or the
// named variable when As is set). A missing key yields a null body or leaves the
// variable unset.
func (p *objectRead) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	key, err := p.key.EvalString(messageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("object-read key: %w", err)
	}

	kv := core.RuntimeServicesFromContext(ctx).KV()
	entry, ok, err := kv.Get(ctx, core.NamespaceUser, key)
	if err != nil {
		return nil, fmt.Errorf("object-read %q: %w", key, err)
	}
	if !ok {
		if p.as == "" {
			msg.Body = nil
		}
		return msg, nil
	}

	if p.as != "" {
		var value any
		if unmarshalErr := json.Unmarshal(entry.Value, &value); unmarshalErr != nil {
			return nil, fmt.Errorf("object-read %q: decode value: %w", key, unmarshalErr)
		}
		msg.Variables.Set(p.as, value)
		return msg, nil
	}
	if setErr := msg.SetBodyJSON(entry.Value); setErr != nil {
		return nil, fmt.Errorf("object-read %q: %w", key, setErr)
	}
	return msg, nil
}

// objectDeleteSettings configures the object-delete block.
type objectDeleteSettings struct {
	// Key is a CEL expression evaluated to the object key (required).
	Key string `json:"key"`
}

// objectDelete removes an object from the user KV namespace by evaluated key.
type objectDelete struct {
	key *expr.Program
	env map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newObjectDelete(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg objectDeleteSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Key == "" {
		return nil, errors.New("object-delete requires a key expression")
	}
	key, err := expr.Compile(cfg.Key, exprVarNames...)
	if err != nil {
		return nil, err
	}
	return &objectDelete{key: key, env: envActivation(deps.Env)}, nil
}

// Process evaluates the key and deletes the object unconditionally (version 0), so
// the delete is idempotent: a missing key is not an error. The message passes
// through unchanged.
func (p *objectDelete) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	key, err := p.key.EvalString(messageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("object-delete key: %w", err)
	}

	kv := core.RuntimeServicesFromContext(ctx).KV()
	if err := kv.Delete(ctx, core.NamespaceUser, key, 0); err != nil {
		return nil, fmt.Errorf("object-delete %q: %w", key, err)
	}
	return msg, nil
}
