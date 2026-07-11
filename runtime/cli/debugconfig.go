package main

// This file holds --run-debug-config: one file describing a whole debugging run —
// what to send, where to stop, what to watch, what to stand in for — so a session
// that took a page of flags to type is reproducible, diffable, and can live in the
// repo next to the flow it exercises.

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// debugConfig is a debugging run, written down. It is YAML, and JSON is accepted for
// free because JSON is a subset of YAML — so the same --mocks blob that works on the
// command line can be pasted into the file.
//
//	input:
//	  data: { orderId: 42 }
//	  vars: { x-api-key: dev-key }
//	breakAt: orders.charge
//	spies:
//	  - orders.seed
//	  - orders.fanout[audit].log-it
//	mocks:
//	  orders.charge:
//	    cases:
//	      - when: 'body.amount > 100'
//	        error: card declined
//	    default:
//	      drop: true
type debugConfig struct {
	Input   debugInput               `yaml:"input"   json:"input"`
	BreakAt string                   `yaml:"breakAt" json:"breakAt"`
	Spies   []string                 `yaml:"spies"   json:"spies"`
	Mocks   map[string]core.MockSpec `yaml:"mocks"   json:"mocks"`
}

// debugInput is what the flow is called with. Data is a structured value rather than
// a JSON string, so the body reads as YAML like the rest of the file; it is
// re-encoded to JSON on the way to the message.
type debugInput struct {
	Data any             `yaml:"data" json:"data"`
	Vars types.Variables `yaml:"vars" json:"vars"`
}

// loadDebugConfig reads the debug config at path. An empty path yields the zero
// config, which contributes nothing — so the caller can merge unconditionally.
//
// Unknown fields are rejected. A debug config is something a user hand-writes, and a
// typo'd key that is silently ignored would look exactly like a feature that does not
// work: a misspelled `spys:` would report no records at all, and the user would go
// looking for the bug in their flow.
func loadDebugConfig(path string) (debugConfig, error) {
	if path == "" {
		return debugConfig{}, nil
	}

	raw, err := os.ReadFile(path) //nolint:gosec // G304: operator-provided config path, like --config
	if err != nil {
		return debugConfig{}, fmt.Errorf("read debug config %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)

	var cfg debugConfig
	if err := decoder.Decode(&cfg); err != nil {
		return debugConfig{}, fmt.Errorf("parse debug config %q: %w", path, err)
	}
	return cfg, nil
}

// body returns the input body as JSON bytes, or nil when the config carries none.
func (c debugConfig) body() ([]byte, error) {
	if c.Input.Data == nil {
		return nil, nil
	}
	raw, err := json.Marshal(c.Input.Data)
	if err != nil {
		return nil, fmt.Errorf("encode debug config input.data: %w", err)
	}
	return raw, nil
}

// spies returns the collector for the config's spy addresses, or nil when it lists
// none.
func (c debugConfig) spies() (*core.Spies, error) {
	if len(c.Spies) == 0 {
		return nil, nil //nolint:nilnil // no spies listed is not an error: there is simply no collector
	}
	spies, err := core.NewSpies(c.Spies)
	if err != nil {
		return nil, fmt.Errorf("debug config: %w", err)
	}
	return spies, nil
}

// mocks returns the mock set for the config's specs, or nil when it lists none.
func (c debugConfig) mocks() (*core.Mocks, error) {
	if len(c.Mocks) == 0 {
		return nil, nil //nolint:nilnil // no mocks listed is not an error: there is simply nothing to mock
	}
	mocks, err := core.NewMocks(c.Mocks)
	if err != nil {
		return nil, fmt.Errorf("debug config: %w", err)
	}
	return mocks, nil
}
