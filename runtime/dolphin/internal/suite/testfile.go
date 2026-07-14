// Package suite is what a _test.yaml is: the format, its validation, and the rules
// for pairing a suite with the flows it exercises.
//
// A suite is a debug config with assertions bolted on. The input, the mocks and the
// spied addresses are literally the sections `octo invoke --run-debug-config` already
// reads, which is why a case can be handed to octo without inventing a second format
// on the wire — and why the mocks here are core.MockSpec, the engine's own type,
// rather than a copy of it.
package suite

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// DefaultCaseTimeout bounds one case, matching `octo invoke`'s own default.
const DefaultCaseTimeout = 30 * time.Second

// inputKeys are the keys an inline `input:` may carry. They are checked by hand
// because a custom unmarshaller does not inherit the decoder's KnownFields setting —
// see knownKeys.
var inputKeys = []string{"data", "vars"}

// File is one _test.yaml: the flow under test, and the cases that exercise it.
type File struct {
	// Flow is the flow under test. Every case in the file calls it.
	Flow string `yaml:"flow" json:"flow"`
	// Inputs are named messages the cases share, so the body that means "a vip order"
	// is written once and referred to by name.
	Inputs map[string]Input `yaml:"inputs" json:"inputs"`
	// Mocks stand in for blocks in every case, unless the case overrides the address.
	// This is where "no test in this file ever reaches the payment API" is said once.
	Mocks map[string]core.MockSpec `yaml:"mocks" json:"mocks"`
	// Timeout bounds every case in the file. A case may shorten or lengthen it.
	Timeout time.Duration `yaml:"timeout" json:"timeout"`
	Cases   []Case        `yaml:"cases" json:"cases"`

	// Path is where the file was read from. It is not part of the format — it is
	// carried so a failure can name the file it came from.
	Path string `yaml:"-" json:"-"`
}

// Input is what a flow is called with — the same shape as a debug config's `input:`,
// because that is exactly what it becomes. Sources are not started under invoke, so
// nothing populates the message for you: Vars is how a case seeds what a source
// normally would (the http source copies request headers into them).
type Input struct {
	Data any             `yaml:"data" json:"data"`
	Vars types.Variables `yaml:"vars" json:"vars"`
}

// Case is one scenario: what to send, what to stand in for, and what should have
// happened.
type Case struct {
	Name string `yaml:"name" json:"name"`
	// Input is either the name of a shared input or an inline one.
	Input CaseInput `yaml:"input" json:"input"`
	// Mocks override the file's, per address and whole-spec. Replacing rather than
	// merging case by case is the same rule `octo invoke` uses for a flag over a debug
	// config section: a mock a case declares says exactly what that block will do,
	// without the reader having to hold the file's version in their head too.
	Mocks map[string]core.MockSpec `yaml:"mocks" json:"mocks"`
	// Expect is what the flow should have produced. An absent Expect still asserts
	// something: that the flow completed and did not fail.
	Expect Expectation `yaml:"expect" json:"expect"`
	// Spies are the blocks to watch, and what each should have seen.
	Spies   map[string]SpyExpect `yaml:"spies" json:"spies"`
	Timeout time.Duration        `yaml:"timeout" json:"timeout"`
	// Skip is a reason. The case is reported as skipped and never run.
	Skip string `yaml:"skip" json:"skip"`
}

// CaseInput is a case's input: either the name of one declared in `inputs:`, or an
// inline one written out where it is used. Both are common — a body reused by six
// cases wants a name, a body used once does not — so the format takes either.
type CaseInput struct {
	Name   string
	Inline *Input
}

// UnmarshalYAML accepts a scalar (a name) or a mapping (an inline input).
func (c *CaseInput) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if err := node.Decode(&c.Name); err != nil {
			return fmt.Errorf("read `input` as the name of a shared input: %w", err)
		}
		return nil
	case yaml.MappingNode:
		if err := knownKeys(node, inputKeys); err != nil {
			return err
		}
		var inline Input
		if err := node.Decode(&inline); err != nil {
			return fmt.Errorf("read `input` as an inline input: %w", err)
		}
		c.Inline = &inline
		return nil
	default:
		return fmt.Errorf("line %d: `input` is the name of a shared input, or an inline {data, vars}", node.Line)
	}
}

// MessageExpect is what a message should look like: the body exactly, the variables it
// must at least carry, and any CEL that must hold over it.
//
// Body is an exact deep-equal and Vars a subset, and the asymmetry is deliberate. The
// body is the flow's answer, and a test earns its keep by failing when a new field
// appears in it. Variables are scratch space the engine and the blocks add to —
// vars.error, vars.httpStatus, whatever a set-variable left behind — so pinning the
// whole map would break on every unrelated change. `that: ['vars.size() == 2']` is
// there for when you do want it exact.
type MessageExpect struct {
	Body any             `yaml:"body" json:"body"`
	Vars types.Variables `yaml:"vars" json:"vars"`
	// That is CEL over the message, every expression of which must be true. It is the
	// same CEL a mock's `when` and every block in the flow uses, over the same
	// variables — one expression language, learned once.
	That []string `yaml:"that" json:"that"`
}

// IsEmpty reports whether the matcher asserts nothing.
func (m MessageExpect) IsEmpty() bool {
	return m.Body == nil && len(m.Vars) == 0 && len(m.That) == 0
}

// Expectation is what the flow should have done: produced a message, dropped it, or
// failed. Exactly as a block does, and for the same reason — those are the three
// things a flow can do.
type Expectation struct {
	MessageExpect `yaml:",inline" json:",inline"`
	// Dropped asserts the flow filtered the message out and returned nothing.
	Dropped bool `yaml:"dropped" json:"dropped"`
	// Error asserts the flow failed, with a message containing this text.
	Error string `yaml:"error" json:"error"`
}

// SpyExpect is what one watched block should have seen.
type SpyExpect struct {
	// Count is the exact number of crossings. It is a pointer because 0 is an
	// assertion — "this block never ran" — and not the absence of one.
	Count *int `yaml:"count" json:"count"`
	// Records are positional: the i-th crossing. Asserting on fewer crossings than
	// happened is fine; naming more than happened is a failure.
	Records []RecordExpect `yaml:"records" json:"records"`
}

// RecordExpect is one crossing of a spied block: what went in, and what came out.
//
// Input is always available, even for a block that failed — the spy clones the message
// before running the block, precisely so a failure still shows what it was carrying.
// Output, Dropped and Error are the three ways out, and a crossing has exactly one.
type RecordExpect struct {
	Input   *MessageExpect `yaml:"input" json:"input"`
	Output  *MessageExpect `yaml:"output" json:"output"`
	Dropped bool           `yaml:"dropped" json:"dropped"`
	Error   string         `yaml:"error" json:"error"`
}

// Load reads and validates the suite at path.
//
// Unknown fields are rejected. A test file is hand-written, and a typo'd key that was
// silently ignored would look exactly like a test that passes: a misspelled `spys:`
// would watch nothing, assert nothing, and go green — which is worse than having no
// test at all.
func Load(path string) (File, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: a user-named test file, like --config
	if err != nil {
		return File{}, fmt.Errorf("read test file %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)

	var file File
	if err := decoder.Decode(&file); err != nil {
		return File{}, fmt.Errorf("parse test file %q: %w", path, err)
	}
	file.Path = path

	if err := file.validate(); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

// validate rejects a suite dolphin could not run, or could run but should not: a case
// naming an input that does not exist, two cases with the same name, an expectation
// that contradicts itself. Everything it can catch, it catches here — before any octo
// is spawned — so a broken suite fails as the bad file it is rather than as a
// mysterious failure in case seven.
func (f File) validate() error {
	if f.Flow == "" {
		return fmt.Errorf("needs a `flow`: the flow every case in the file calls")
	}
	if len(f.Cases) == 0 {
		return fmt.Errorf("has no `cases`")
	}
	if err := validateMocks(f.Mocks); err != nil {
		return err
	}

	seen := make(map[string]bool, len(f.Cases))
	for i, c := range f.Cases {
		// A case is named before it is numbered: the name is what a report, a JUnit XML
		// and a person all identify it by, so an unnamed one has nothing to be called.
		if c.Name == "" {
			return fmt.Errorf("case %d needs a `name`", i)
		}
		if seen[c.Name] {
			return fmt.Errorf("two cases are named %q", c.Name)
		}
		seen[c.Name] = true

		if err := f.validateCase(c); err != nil {
			return fmt.Errorf("case %q: %w", c.Name, err)
		}
	}
	return nil
}

// validateCase checks one case against the file it lives in.
func (f File) validateCase(c Case) error {
	if c.Input.Name != "" {
		if _, ok := f.Inputs[c.Input.Name]; !ok {
			return fmt.Errorf("input %q is not declared in `inputs` (have: %s)",
				c.Input.Name, listOr(sortedInputNames(f.Inputs), "none"))
		}
	}
	if err := validateMocks(c.Mocks); err != nil {
		return err
	}
	if err := c.Expect.validate(); err != nil {
		return fmt.Errorf("expect: %w", err)
	}
	for _, address := range SpiedBy(c) {
		if strings.TrimSpace(address) == "" {
			return fmt.Errorf("a spy has an empty block address")
		}
		if err := c.Spies[address].validate(); err != nil {
			return fmt.Errorf("spy %q: %w", address, err)
		}
	}
	return nil
}

// validate pins the one-outcome rule on an expectation: a flow either produced a
// message, dropped it, or failed. A case expecting a body AND a failure is not a
// stricter test, it is an impossible one — under `error` the flow returned no message
// for a body to be compared against — so it is a mistake in the suite, and saying so
// beats reporting it every run as a test that cannot pass.
func (e Expectation) validate() error {
	if e.Dropped && e.Error != "" {
		return fmt.Errorf("expects both `dropped` and `error`: a flow that failed did not filter the message out")
	}
	// outcome is what the case says the flow did instead of returning a message.
	outcome, past := "dropped", "dropped the message"
	if e.Error != "" {
		outcome, past = "error", "failed"
	}
	if (e.Dropped || e.Error != "") && !e.IsEmpty() {
		return fmt.Errorf("expects `%s` as well as the message the flow returned, but a flow that %s returned none",
			outcome, past)
	}
	return nil
}

// validate checks one spy's expectations.
func (s SpyExpect) validate() error {
	if s.Count != nil && *s.Count < 0 {
		return fmt.Errorf("count is negative")
	}
	// Naming more records than the count asserts is a contradiction the suite can never
	// satisfy, and it is nearly always a miscount rather than an intent.
	if s.Count != nil && len(s.Records) > *s.Count {
		return fmt.Errorf("asserts on %d records but expects a count of %d", len(s.Records), *s.Count)
	}
	for i, record := range s.Records {
		if err := record.validate(); err != nil {
			return fmt.Errorf("record %d: %w", i, err)
		}
	}
	return nil
}

// validate pins the one-outcome rule on a spied crossing. Unlike an expectation, the
// input is always fair game: the spy clones the message before running the block, so
// even a block that failed shows what it was carrying when it did.
func (r RecordExpect) validate() error {
	var outcomes []string
	if r.Output != nil {
		outcomes = append(outcomes, "output")
	}
	if r.Dropped {
		outcomes = append(outcomes, "dropped")
	}
	if r.Error != "" {
		outcomes = append(outcomes, "error")
	}
	if len(outcomes) > 1 {
		return fmt.Errorf("expects more than one outcome (%s): a block either returns a message, fails, or drops it",
			strings.Join(outcomes, ", "))
	}
	return nil
}

// validateMocks rejects a mock spec the engine would refuse, using the engine's own
// validation — so a suite is told about a bad spec here, by dolphin, rather than by an
// octo it spawned three cases later.
func validateMocks(specs map[string]core.MockSpec) error {
	if len(specs) == 0 {
		return nil
	}
	if _, err := core.NewMocks(specs); err != nil {
		return err //nolint:wrapcheck // the engine's message already names the mock and the case
	}
	return nil
}

// InputFor resolves a case's input against the file's shared ones. A case with no
// input at all calls the flow with an empty message, which is a legitimate thing to
// test.
func (f File) InputFor(c Case) Input {
	if c.Input.Inline != nil {
		return *c.Input.Inline
	}
	if c.Input.Name != "" {
		return f.Inputs[c.Input.Name] // validate() proved it is there
	}
	return Input{}
}

// MocksFor is the mocks one case runs under: the file's, with the case's replacing
// them address by address.
func (f File) MocksFor(c Case) map[string]core.MockSpec {
	if len(f.Mocks) == 0 && len(c.Mocks) == 0 {
		return nil
	}
	merged := make(map[string]core.MockSpec, len(f.Mocks)+len(c.Mocks))
	for address, spec := range f.Mocks {
		merged[address] = spec
	}
	for address, spec := range c.Mocks {
		merged[address] = spec
	}
	return merged
}

// TimeoutFor is how long a case may take: its own, else the file's, else the default.
func (f File) TimeoutFor(c Case) time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	if f.Timeout > 0 {
		return f.Timeout
	}
	return DefaultCaseTimeout
}

// SpiedBy returns the addresses a case watches, sorted so a run is reproducible: two
// runs of the same suite spawn octo with identical arguments, which is what makes the
// command a failure prints actually be the command that failed.
func SpiedBy(c Case) []string {
	addresses := make([]string, 0, len(c.Spies))
	for address := range c.Spies {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	return addresses
}

// knownKeys rejects a mapping key that is not in allowed.
//
// It exists because a custom UnmarshalYAML does NOT inherit the decoder's KnownFields
// setting: node.Decode would happily ignore a typo'd `daat:` inside an inline input,
// in a file where every other typo is caught. One field that silently swallows
// mistakes is worse than none, because it is the one nobody thinks to check.
func knownKeys(node *yaml.Node, allowed []string) error {
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		if !contains(allowed, key) {
			return fmt.Errorf("line %d: field %s not found in an input (want %s)",
				node.Content[i].Line, key, strings.Join(allowed, " or "))
		}
	}
	return nil
}

func contains(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}

// sortedInputNames returns the declared input names, sorted, for an error message.
func sortedInputNames(inputs map[string]Input) []string {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// listOr renders names as a comma-separated list, or empty when there are none.
func listOr(names []string, empty string) string {
	if len(names) == 0 {
		return empty
	}
	return strings.Join(names, ", ")
}
