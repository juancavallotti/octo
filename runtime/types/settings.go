package types

import (
	"encoding/json"
	"fmt"
)

// Settings is a connector- or processor-specific configuration map decoded from
// the YAML config. Components read it in one of two ways: project the whole map
// onto a typed struct with Decode (the preferred path, so each component owns its
// own settings shape), or read individual keys through the typed accessors, which
// share Variables' coercion policy.
type Settings map[string]any

// Decode projects the settings onto target, a non-nil pointer to a struct, by
// round-tripping through JSON — the same bridge Message.DecodeBody uses. Each
// component declares its own settings struct with json tags matching the YAML
// keys, and a value of the wrong type surfaces as a decode error at startup.
func (s Settings) Decode(target any) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}
	return nil
}

// String reads key as a string. ok is false if the key is absent or not a
// string.
func (s Settings) String(key string) (string, bool) { return Variables(s).String(key) }

// Int reads key as an int, accepting the int, int64 and float64 forms a YAML or
// JSON decoder may produce.
func (s Settings) Int(key string) (int, bool) { return Variables(s).Int(key) }

// Bool reads key as a bool.
func (s Settings) Bool(key string) (bool, bool) { return Variables(s).Bool(key) }

// Float reads key as a float64.
func (s Settings) Float(key string) (float64, bool) { return Variables(s).Float(key) }
