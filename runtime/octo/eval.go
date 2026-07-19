package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// This file holds `octo eval`: evaluating one CEL expression against a message built
// from the flags, without running a flow.

// evalOutcome is the JSON envelope `octo eval` prints on stdout. OK distinguishes a
// successful evaluation (Result holds the JSON-native value, which may itself be
// false/0/null) from a compile or eval failure (Error holds the message). Both cases
// exit 0 — a bad expression is a normal result, not a CLI failure — so a consumer can
// parse the envelope rather than inspecting exit codes. Result is always emitted (never
// omitempty) so a legitimate falsey result is not confused with its absence.
type evalOutcome struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// evalCommand evaluates a CEL expression against an ad-hoc message, without a config
// or any runtime services — CEL compilation and evaluation are pure. The --data object
// is bound to `body` (like invoke's request body), --vars to `vars`, and --env to
// `env`; the remaining message variables (eventID, correlationID, now) get their
// defaults. It prints an evalOutcome envelope so the result and the error path are
// unambiguous.
func evalCommand(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	expression := fs.String("expr", "", "CEL expression to evaluate")
	data := fs.String("data", "", "JSON object bound to body (reads stdin when omitted)")
	varsJSON := fs.String("vars", "", "JSON object bound to vars")
	envJSON := fs.String("env", "", "JSON object bound to env")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("parse eval flags: %w", err)
	}
	if *expression == "" {
		return errors.New("expression is required (-expr)")
	}

	body, err := resolveData(*data)
	if err != nil {
		return err
	}
	vars, err := parseVariables(*varsJSON)
	if err != nil {
		return err
	}
	msg, err := buildMessage(body, vars, "")
	if err != nil {
		return err
	}
	// A non-nil env map keeps env.NAME a missing-key error rather than a null-deref,
	// matching how a real run materializes its resolved env (see expr.EnvActivation).
	env := map[string]any{}
	if *envJSON != "" {
		if err := json.Unmarshal([]byte(*envJSON), &env); err != nil {
			return fmt.Errorf("parse -env JSON: %w", err)
		}
	}

	out, err := json.Marshal(evalExpression(*expression, msg, env))
	if err != nil {
		return fmt.Errorf("marshal eval outcome: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// evalExpression compiles expression through the single message-CEL seam and evaluates
// it against msg and env, returning the envelope octo eval prints. A compile or eval
// failure is reported as an OK=false outcome (not a Go error) so the caller prints it
// and exits 0. A nil resource loader is passed: CompileMessage substitutes a no-op
// loader, so standalone evaluation has no integration resources (templateResource is
// unavailable) — expressions over body/vars/env/eventID/correlationID/now still work.
func evalExpression(expression string, msg *types.Message, env map[string]any) evalOutcome {
	program, err := expr.CompileMessage(nil, expression)
	if err != nil {
		return evalOutcome{OK: false, Error: err.Error()}
	}
	result, err := program.Eval(expr.MessageActivation(msg, env))
	if err != nil {
		return evalOutcome{OK: false, Error: err.Error()}
	}
	return evalOutcome{OK: true, Result: result}
}
