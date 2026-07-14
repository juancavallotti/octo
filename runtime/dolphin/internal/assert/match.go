// Package assert decides whether a flow did what a case said it would, and says what
// was wrong when it did not.
//
// It produces failures, never booleans. "The case failed" is not a useful thing to be
// told; "expect.body: want {…}, got {…}" is, and a matcher that only returned false
// would have thrown away the one thing the reader needs. Everything here is written to
// that end.
package assert

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
	"github.com/juancavallotti/octo/runtime/types"
)

// message checks a message against a matcher, prefixing each failure with where it came
// from ("expect", "spy orders.charge record 0 input") so a report never leaves the
// reader guessing which of a case's several assertions broke.
func message(where string, want suite.MessageExpect, got *types.Message) []string {
	if got == nil {
		if want.IsEmpty() {
			return nil
		}
		return []string{fmt.Sprintf("%s: there is no message to check", where)}
	}

	var failures []string
	if want.Body != nil {
		failures = append(failures, matchBody(where, want.Body, got.Body)...)
	}
	failures = append(failures, matchVars(where, want.Vars, got.Variables)...)
	failures = append(failures, matchThat(where, want.That, got)...)
	return failures
}

// matchBody compares the body exactly. The body is the flow's answer, and a test earns
// its keep by failing when a field appears in it that nobody expected.
func matchBody(where string, want, got any) []string {
	normalized, err := normalize(want)
	if err != nil {
		return []string{fmt.Sprintf("%s.body: %v", where, err)}
	}
	if reflect.DeepEqual(normalized, got) {
		return nil
	}
	return []string{fmt.Sprintf("%s.body:\n      want: %s\n      got:  %s", where, render(normalized), render(got))}
}

// matchVars checks that each named variable is there and equal. It is a SUBSET, unlike
// the body, and deliberately: variables are scratch space the engine and the blocks add
// to — vars.error, vars.httpStatus, whatever a set-variable left behind — so pinning the
// whole map would break on every unrelated change. `that: ['vars.size() == 2']` is there
// for when you do want it exact.
func matchVars(where string, want types.Variables, got types.Variables) []string {
	var failures []string
	for _, name := range sortedNames(want) {
		actual, ok := got[name]
		if !ok {
			failures = append(failures, fmt.Sprintf("%s.vars.%s: not set (vars: %s)",
				where, name, listOr(sortedNames(got), "none")))
			continue
		}
		normalized, err := normalize(want[name])
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s.vars.%s: %v", where, name, err))
			continue
		}
		if !reflect.DeepEqual(normalized, actual) {
			failures = append(failures, fmt.Sprintf("%s.vars.%s: want %s, got %s",
				where, name, render(normalized), render(actual)))
		}
	}
	return failures
}

// matchThat evaluates each CEL expression against the message. Every one must be true.
//
// This is the same CEL a mock's `when` and every block in the flow uses, compiled
// through the same seam (expr.CompileMessage) against the same variables — so an
// assertion is written the way the flow it tests is written, and there is one expression
// language to learn rather than two.
//
// env is not bound: dolphin never loads the config — octo does, in the child process —
// so there is no resolved environment to activate. An expression referencing env.X fails
// here, and says so, rather than quietly evaluating to nothing.
func matchThat(where string, expressions []string, msg *types.Message) []string {
	var failures []string
	for _, expression := range expressions {
		program, err := expr.CompileMessage(nil, expression)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s.that: %s does not compile: %v", where, expression, err))
			continue
		}

		value, err := program.Eval(expr.MessageActivation(msg, nil))
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s.that: %s did not evaluate: %v", where, expression, err))
			continue
		}

		// An expression that returns a non-boolean is a mistake, not a falsy value.
		// `body.total` is not an assertion, and silently reading it as one — truthy
		// because it is non-zero — would pass a test that checks nothing.
		holds, ok := value.(bool)
		if !ok {
			failures = append(failures, fmt.Sprintf("%s.that: %s is not a true/false expression (it returned %s)",
				where, expression, render(value)))
			continue
		}
		if !holds {
			failures = append(failures, fmt.Sprintf("%s.that: %s is false", where, expression))
		}
	}
	return failures
}

// normalize puts a value from the test file into the shape the message carries.
//
// It is not cosmetic. The expected body is decoded from YAML, where 42 is an int; the
// actual body came back as JSON, where 42 is a float64. reflect.DeepEqual says those
// differ, so without this every numeric assertion in every suite would fail — and the
// report would show `want: 42, got: 42`, which is the worst kind of failure to debug.
// Round-tripping the expected value through JSON gives it the same types the message has.
func normalize(value any) (any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("cannot be compared: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, fmt.Errorf("cannot be compared: %w", err)
	}
	return normalized, nil
}

// render shows a value the way the message carries it, as compact JSON, so a diff
// between what was wanted and what arrived is read in one line rather than in Go's
// map[string]interface {} spelling.
func render(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

// sortedNames returns a variable map's keys, sorted, so a report reads the same twice.
func sortedNames(vars types.Variables) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
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
