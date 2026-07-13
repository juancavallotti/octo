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
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// objectWriteAttempts bounds the optimistic-concurrency retry loop of an
// object-write: a write re-reads the current version and retries on a version
// conflict, so a concurrent writer cannot make it spin forever.
const objectWriteAttempts = 5

func init() {
	core.MustRegisterBlock("object-read", newObjectRead)
	core.MustRegisterBlock("object-write", newObjectWrite)
	core.MustRegisterBlock("object-delete", newObjectDelete)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "object-read",
		Label:    "Object Read",
		Category: core.CategoryProcessor,
		Group:    groupStorageCache,
		Icon:     "HardDriveDownload",
		Description: "Read an object from the runtime store into the body or a variable. When the " +
			"key is absent and `default` is set, the default is folded in exactly like a hit; " +
			"otherwise the body is nulled (body mode) or the variable is left unset. Set " +
			"`existsVar` to also record whether the key was found.",
		Config: reflect.TypeFor[objectReadSettings](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "object-write",
		Label:       "Object Write",
		Category:    core.CategoryProcessor,
		Group:       groupStorageCache,
		Icon:        "HardDriveUpload",
		Description: "Write an object to the runtime store under a key.",
		Config:      reflect.TypeFor[objectWriteSettings](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "object-delete",
		Label:       "Object Delete",
		Category:    core.CategoryProcessor,
		Group:       groupStorageCache,
		Icon:        "Trash2",
		Description: "Delete an object from the runtime store by key.",
		Config:      reflect.TypeFor[objectDeleteSettings](),
	})
}

// objectWriteSettings configures the object-write block.
type objectWriteSettings struct {
	// CEL expression evaluated to the object key.
	Key string `json:"key" octo:"label=Key,type=cel,required"`
	// CEL expression for the value to store; empty stores the whole body.
	Value string `json:"value" octo:"label=Value,type=cel"`
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
	key, err := expr.CompileMessage(deps.Resources, cfg.Key)
	if err != nil {
		return nil, err
	}

	block := &objectWrite{key: key, env: expr.EnvActivation(deps.Env)}
	if cfg.Value != "" {
		value, valueErr := expr.CompileMessage(deps.Resources, cfg.Value)
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
	activation := expr.MessageActivation(msg, p.env)
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
	// CEL expression evaluated to the object key.
	Key string `json:"key" octo:"label=Key,type=cel,required"`
	// Variable to store the object under; empty replaces the body.
	As string `json:"as" octo:"label=As variable"`
	// CEL expression evaluated when the key is absent; its result is folded in like
	// a hit (into the variable, or the body). Empty leaves a miss as a null body /
	// unset variable. Set existsVar to tell a default apart from a hit.
	Default string `json:"default" octo:"label=Default,type=cel"`
	// When set, names a variable the block writes a boolean into: true when the key
	// was found, false when the default/null path was taken. Empty writes no
	// variable (the legacy behavior).
	ExistsVar string `json:"existsVar" octo:"label=Exists variable"`
}

// objectRead reads an object from the user KV namespace into the message body or
// a named variable.
type objectRead struct {
	key         *expr.Program
	as          string        // empty folds the object into the body
	defaultProg *expr.Program // nil leaves a miss as null/unset
	existsVar   string        // empty writes no presence variable
	env         map[string]any
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
	key, err := expr.CompileMessage(deps.Resources, cfg.Key)
	if err != nil {
		return nil, err
	}

	block := &objectRead{key: key, as: cfg.As, existsVar: cfg.ExistsVar, env: expr.EnvActivation(deps.Env)}
	if cfg.Default != "" {
		defaultProg, defErr := expr.CompileMessage(deps.Resources, cfg.Default)
		if defErr != nil {
			return nil, defErr
		}
		block.defaultProg = defaultProg
	}
	return block, nil
}

// Process evaluates the key, reads the object, and folds it into the body (or the
// named variable when As is set). When ExistsVar is set it also writes a boolean
// reporting whether the key was found. A miss folds in the default expression when
// one is configured; otherwise it keeps the legacy behavior (null body / unset
// variable).
func (p *objectRead) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	key, err := p.key.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("object-read key: %w", err)
	}

	kv := core.RuntimeServicesFromContext(ctx).KV()
	entry, ok, err := kv.Get(ctx, core.NamespaceUser, key)
	if err != nil {
		return nil, fmt.Errorf("object-read %q: %w", key, err)
	}
	if p.existsVar != "" {
		msg.Variables.Set(p.existsVar, ok)
	}

	if !ok {
		return p.miss(msg, key, activation)
	}

	var value any
	if unmarshalErr := json.Unmarshal(entry.Value, &value); unmarshalErr != nil {
		return nil, fmt.Errorf("object-read %q: decode value: %w", key, unmarshalErr)
	}
	p.fold(msg, value)
	return msg, nil
}

// miss handles an absent key: it folds in the default expression when one is
// configured, otherwise keeps the legacy behavior (null body in body mode, an
// unset variable in As mode).
func (p *objectRead) miss(msg *types.Message, key string, activation map[string]any) (*types.Message, error) {
	if p.defaultProg == nil {
		if p.as == "" {
			msg.Body = nil
		}
		return msg, nil
	}
	value, err := p.defaultProg.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("object-read %q: default: %w", key, err)
	}
	p.fold(msg, value)
	return msg, nil
}

// fold places value into the named variable when As is set, or into the body
// otherwise — the single target rule shared by a hit and a default.
func (p *objectRead) fold(msg *types.Message, value any) {
	if p.as != "" {
		msg.Variables.Set(p.as, value)
		return
	}
	msg.Body = value
}

// objectDeleteSettings configures the object-delete block.
type objectDeleteSettings struct {
	// CEL expression evaluated to the object key.
	Key string `json:"key" octo:"label=Key,type=cel,required"`
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
	key, err := expr.CompileMessage(deps.Resources, cfg.Key)
	if err != nil {
		return nil, err
	}
	return &objectDelete{key: key, env: expr.EnvActivation(deps.Env)}, nil
}

// Process evaluates the key and deletes the object unconditionally (version 0), so
// the delete is idempotent: a missing key is not an error. The message passes
// through unchanged.
func (p *objectDelete) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	key, err := p.key.EvalString(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("object-delete key: %w", err)
	}

	kv := core.RuntimeServicesFromContext(ctx).KV()
	if err := kv.Delete(ctx, core.NamespaceUser, key, 0); err != nil {
		return nil, fmt.Errorf("object-delete %q: %w", key, err)
	}
	return msg, nil
}
