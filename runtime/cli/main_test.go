package main

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/types"
)

// evalMessage builds a message for evalExpression tests: body from a decoded value and
// optional vars, mirroring what evalCommand assembles from --data/--vars.
func evalMessage(t *testing.T, body any, vars types.Variables) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	if vars != nil {
		msg.Variables = vars
	}
	return msg
}

// TestEvalExpression covers the standalone CEL evaluator: a successful result over
// body/vars/env, a preserved falsey result, and the two failure modes (compile and
// eval), each reported as an OK=false outcome rather than a Go error.
func TestEvalExpression(t *testing.T) {
	t.Run("body arithmetic", func(t *testing.T) {
		got := evalExpression("body.amount + 1.0", evalMessage(t, map[string]any{"amount": 4.0}, nil), nil)
		if !got.OK || got.Result != 5.0 {
			t.Errorf("evalExpression = %+v, want ok result 5", got)
		}
	})

	t.Run("vars and env references", func(t *testing.T) {
		msg := evalMessage(t, map[string]any{}, types.Variables{"tier": "gold"})
		env := map[string]any{"REGION": "us"}
		got := evalExpression(`vars.tier == "gold" && env.REGION == "us"`, msg, env)
		if !got.OK || got.Result != true {
			t.Errorf("evalExpression = %+v, want ok result true", got)
		}
	})

	t.Run("falsey result is preserved", func(t *testing.T) {
		got := evalExpression("body.amount > 100", evalMessage(t, map[string]any{"amount": 5.0}, nil), nil)
		if !got.OK || got.Result != false {
			t.Errorf("evalExpression = %+v, want ok result false", got)
		}
	})

	t.Run("compile error", func(t *testing.T) {
		got := evalExpression("body.", evalMessage(t, map[string]any{}, nil), nil)
		if got.OK || got.Error == "" {
			t.Errorf("evalExpression = %+v, want ok=false with error", got)
		}
	})

	t.Run("eval error on missing key", func(t *testing.T) {
		got := evalExpression("body.missing.deep", evalMessage(t, map[string]any{}, nil), nil)
		if got.OK || got.Error == "" {
			t.Errorf("evalExpression = %+v, want ok=false with error", got)
		}
	})
}

// TestEvalCommandRequiresExpr checks that eval rejects a missing expression before it
// tries to evaluate, so a caller gets a clear usage error rather than an empty run.
func TestEvalCommandRequiresExpr(t *testing.T) {
	if err := evalCommand([]string{"--data", "{}"}); err == nil {
		t.Error("evalCommand without -expr = nil, want error")
	}
}

// TestRunVersion checks that every version invocation form is handled in run()
// and returns nil without falling through to the run/invoke flagsets (which do
// not define the flag and would otherwise error).
func TestRunVersion(t *testing.T) {
	for _, args := range [][]string{
		{"--version"},
		{"-version"},
		{"version"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%q) = %v, want nil", args, err)
		}
	}
}

// TestRunHelp checks that every help form is handled in run() and returns nil
// instead of falling through to a flagset (which would error on -help).
func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"--help"},
		{"-help"},
		{"-h"},
		{"help"},
		{"run", "--help"},
		{"invoke", "--help"},
		{"eval", "--help"},
	} {
		if err := run(args); err != nil {
			t.Errorf("run(%q) = %v, want nil", args, err)
		}
	}
}

// TestVersionLine checks the python/java-style output: name and version always,
// with a "(built ...)" suffix only when a build date is present.
func TestVersionLine(t *testing.T) {
	orig := BuildDate
	t.Cleanup(func() { BuildDate = orig })

	BuildDate = ""
	if got := versionLine(); !strings.HasPrefix(got, "octo "+Version) {
		t.Errorf("versionLine() = %q, want prefix %q", got, "octo "+Version)
	}

	BuildDate = "2026-06-18T22:31:23Z"
	got := versionLine()
	if !strings.HasPrefix(got, "octo "+Version) {
		t.Errorf("versionLine() = %q, want prefix %q", got, "octo "+Version)
	}
	if !strings.Contains(got, "(built "+BuildDate+")") {
		t.Errorf("versionLine() = %q, want build date %q rendered", got, BuildDate)
	}
}
