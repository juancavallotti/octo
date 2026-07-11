package core

import (
	"fmt"
	"sort"
)

// MockCase is one canned outcome of a mocked block: a CEL condition, and what the
// block should do when it holds. Exactly one of Body, Error and Drop is set — a
// block either produces a message, fails, or filters the message out, and a mock
// stands in for a block.
//
// When is evaluated against the message the block *received*, so a mock dispatches
// on its input the way the real block would: `body.amount > 100`, `vars.tier ==
// "vip"`. It is the only expression a mock carries — Body is a literal value, not an
// expression, because a mock is a canned response and the dispatch already happened
// in When.
type MockCase struct {
	When string `json:"when,omitempty" yaml:"when,omitempty"`
	// Body replaces the message body. Vars are set on the message alongside it, for
	// the blocks that report through a variable rather than the body (an http call
	// setting vars.status). Vars without a Body is rejected: it would be a case that
	// says nothing about what the block returned.
	Body any            `json:"body,omitempty" yaml:"body,omitempty"`
	Vars map[string]any `json:"vars,omitempty" yaml:"vars,omitempty"`
	// Error fails the block with this message, which is how an error path is tested
	// without arranging for the real block to fail.
	Error string `json:"error,omitempty" yaml:"error,omitempty"`
	// Drop filters the message out, as a filter block would.
	Drop bool `json:"drop,omitempty" yaml:"drop,omitempty"`
}

// MockSpec is what one addressed block is mocked with: the cases, tried in order,
// and an optional default for when none of them matched.
//
// With no default, a message that matches no case fails the block. That is
// deliberate, and it is not the same as "fall through to the real block": the mock
// replaced the block, so there is no real block left to fall through to. Keeping one
// wired would mean a mocked HTTP call or LLM call could still reach the network and
// spend money on a run the user believes is mocked — which is the failure mocking
// exists to prevent. An unmatched message is a hole in the mock, and saying so is
// more useful than silently doing nothing. The escape hatches are one line: a
// `default`, or a trailing case with `when: "true"`.
type MockSpec struct {
	Cases   []MockCase `json:"cases,omitempty"   yaml:"cases,omitempty"`
	Default *MockCase  `json:"default,omitempty" yaml:"default,omitempty"`
}

// Mocks is the set of blocks to stand in for, keyed by address. Unlike Breakpoint
// and Spies it collects nothing: a mock is an input to the run, not an observation
// of it. It reaches the engine the same way they do — through BlockDeps — so that a
// `type: mock` block cannot be authored in a flow, only injected.
type Mocks struct {
	specs map[string]MockSpec
}

// NewMocks validates the specs and returns the set. An invalid spec is rejected here,
// at the edge, so the flow builder can trust what it is handed.
func NewMocks(specs map[string]MockSpec) (*Mocks, error) {
	for _, address := range sortedKeys(specs) {
		if address == "" {
			return nil, fmt.Errorf("mock address is empty")
		}
		if err := validateSpec(specs[address]); err != nil {
			return nil, fmt.Errorf("mock %q: %w", address, err)
		}
	}
	return &Mocks{specs: specs}, nil
}

// Addresses returns the mocked block addresses, sorted, so injection is deterministic
// and two runs of the same config produce the same tree.
func (m *Mocks) Addresses() []string { return sortedKeys(m.specs) }

// For returns the spec the block at address is mocked with.
func (m *Mocks) For(address string) (MockSpec, bool) {
	spec, ok := m.specs[address]
	return spec, ok
}

// validateSpec rejects a spec the engine could not act on.
func validateSpec(spec MockSpec) error {
	if len(spec.Cases) == 0 && spec.Default == nil {
		return fmt.Errorf("needs at least one case, or a default")
	}
	for i, c := range spec.Cases {
		if c.When == "" {
			return fmt.Errorf("case %d needs a `when` expression (a case with no condition is the `default`)", i)
		}
		if err := validateOutcome(c); err != nil {
			return fmt.Errorf("case %d: %w", i, err)
		}
	}
	if spec.Default == nil {
		return nil
	}
	if spec.Default.When != "" {
		return fmt.Errorf("the default must not have a `when` expression: it is what runs when no case matched")
	}
	if err := validateOutcome(*spec.Default); err != nil {
		return fmt.Errorf("default: %w", err)
	}
	return nil
}

// validateOutcome pins the one-outcome rule: a block either produces a message,
// fails, or drops it, so a case that claims two of those is a mistake in the spec
// rather than something to resolve by precedence.
func validateOutcome(c MockCase) error {
	var set []string
	if c.Body != nil {
		set = append(set, "body")
	}
	if c.Error != "" {
		set = append(set, "error")
	}
	if c.Drop {
		set = append(set, "drop")
	}

	switch len(set) {
	case 0:
		return fmt.Errorf("needs an outcome: one of `body`, `error` or `drop`")
	case 1:
		if len(c.Vars) > 0 && c.Body == nil {
			return fmt.Errorf("`vars` only goes with a `body`: a block that failed or dropped returned no message")
		}
		return nil
	default:
		return fmt.Errorf("has more than one outcome (%v): a block either returns a message, fails, or drops it", set)
	}
}

// sortedKeys returns the map's keys in a stable order.
func sortedKeys(specs map[string]MockSpec) []string {
	keys := make([]string, 0, len(specs))
	for key := range specs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
