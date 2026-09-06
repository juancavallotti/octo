package types

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// FlowConfig is the recursive unit of pipeline composition. The root flow,
// listed under Config.Flows, binds a Source and a worker-pool size; sub-flows
// nested inside a block's slot reuse the same shape but must not set Source,
// Workers, Buffer, Pool, or Error (the engine validates this when it builds the
// slot).
//
// It carries json tags beside the yaml ones because a block decodes its
// sub-flows out of its settings map through the JSON bridge Settings.Decode
// uses, so the two spellings of every key have to agree.
type FlowConfig struct {
	Name    string        `yaml:"name,omitempty" json:"name,omitempty"`
	Source  *SourceConfig `yaml:"source,omitempty" json:"source,omitempty"`
	Process []BlockConfig `yaml:"process" json:"process"`
	// Error is the root flow's error path: when the Process chain returns an
	// error, the runtime exposes it as vars.error and runs this chain; on success
	// its output becomes the flow's result (recovery). It is a bare block chain,
	// like Process. Root flows only.
	Error   []BlockConfig `yaml:"error,omitempty" json:"error,omitempty"`
	Workers int           `yaml:"workers,omitempty" json:"workers,omitempty"`
	Buffer  int           `yaml:"buffer,omitempty" json:"buffer,omitempty"`
	// Pool sizes the shared worker pool the root flow owns and passes down to
	// blocks that schedule work concurrently (e.g. a fork's branches). Root flows
	// only; defaults when unset.
	Pool int `yaml:"pool,omitempty" json:"pool,omitempty"`
}

// SourceConfig binds a flow's entry point to a connector instance and a
// connector-specific source type.
type SourceConfig struct {
	// Connector is the Name of a configured connector instance, not its Type.
	Connector string   `yaml:"connector" json:"connector"`
	Type      string   `yaml:"type" json:"type"`
	Settings  Settings `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// BlockConfig describes one step in a flow: what it is and how it is configured.
// Every block, leaf or composite, has the same shape. What a block does with its
// settings — including which of them are sub-flows to build — is the block's own
// business, declared by its settings struct and read through Settings.Decode.
//
// A block's settings may be written under a `settings:` key or as top-level keys
// beside `type`; decoding folds the two together, so a composite's `then:` and a
// leaf's `settings: {url: …}` are the same thing spelled two ways. A key given in
// both places is an error rather than a silent override.
type BlockConfig struct {
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Ref names a reusable processor defined under Config.Processors. When set,
	// the block takes its type and base settings from that definition; any
	// Settings here override the referenced ones key-by-key. A block sets either
	// Ref or Type, not both (an inline Type equal to the referenced type is the
	// one allowed overlap).
	Ref string `yaml:"ref,omitempty" json:"ref,omitempty"`

	// Settings is everything else the block was given: its scalar settings and,
	// for a block that runs nested chains, the sub-flow slots. A sub-flow arrives
	// here as it was written — a generic map when decoded from a document, or a
	// FlowConfig / []BlockConfig value when the config was assembled in Go — and
	// the block's settings struct types it either way.
	Settings Settings `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// rawBlock is the wire shape of a block: the reserved keys, plus everything else
// as an inline map. Both decoders fold Rest into Settings.
type rawBlock struct {
	Type     string         `yaml:"type,omitempty"`
	Name     string         `yaml:"name,omitempty"`
	Ref      string         `yaml:"ref,omitempty"`
	Settings Settings       `yaml:"settings,omitempty"`
	Rest     map[string]any `yaml:",inline"`
}

// UnmarshalYAML decodes a block from a document, folding any key that is not
// type, name, ref or settings into Settings.
func (b *BlockConfig) UnmarshalYAML(node *yaml.Node) error {
	var raw rawBlock
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("decode block: %w", err)
	}
	return b.fold(raw)
}

// UnmarshalJSON decodes a block the way UnmarshalYAML does. It exists because a
// composite types its sub-flows through the JSON bridge, and a nested block
// written with top-level keys has to fold the same way on that path.
func (b *BlockConfig) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode block: %w", err)
	}
	var raw rawBlock
	for key, value := range fields {
		var err error
		switch key {
		case "type":
			err = json.Unmarshal(value, &raw.Type)
		case "name":
			err = json.Unmarshal(value, &raw.Name)
		case "ref":
			err = json.Unmarshal(value, &raw.Ref)
		case "settings":
			err = json.Unmarshal(value, &raw.Settings)
		default:
			var v any
			if err = json.Unmarshal(value, &v); err == nil {
				if raw.Rest == nil {
					raw.Rest = make(map[string]any)
				}
				raw.Rest[key] = v
			}
		}
		if err != nil {
			return fmt.Errorf("block field %q: %w", key, err)
		}
	}
	return b.fold(raw)
}

// fold merges the inline keys into the settings map. The reserved keys stay
// reserved: a block cannot smuggle a second `type` in through `settings:`.
func (b *BlockConfig) fold(raw rawBlock) error {
	b.Type, b.Name, b.Ref = raw.Type, raw.Name, raw.Ref
	b.Settings = nil
	if len(raw.Settings) == 0 && len(raw.Rest) == 0 {
		return nil
	}
	b.Settings = make(Settings, len(raw.Settings)+len(raw.Rest))
	for key, value := range raw.Settings {
		b.Settings[key] = value
	}
	for key, value := range raw.Rest {
		if _, dup := b.Settings[key]; dup {
			return fmt.Errorf("block %q: %q is given both at the top level and under settings",
				b.label(), key)
		}
		b.Settings[key] = value
	}
	return nil
}

// label is how a block names itself in a decode error: its name, else its type,
// else its ref.
func (b BlockConfig) label() string {
	switch {
	case b.Name != "":
		return b.Name
	case b.Type != "":
		return b.Type
	default:
		return b.Ref
	}
}
