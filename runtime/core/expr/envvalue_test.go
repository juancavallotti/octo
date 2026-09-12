package expr

import (
	"fmt"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// evalEnv compiles `expression` against an env and evaluates it once.
func evalEnv(t *testing.T, expression string, env map[string]string) string {
	t.Helper()
	program, err := CompileMessage(nil, expression)
	if err != nil {
		t.Fatalf("compile %q: %v", expression, err)
	}
	got, err := program.EvalString(MessageActivation(&types.Message{}, EnvActivation(env)))
	if err != nil {
		t.Fatalf("eval %q: %v", expression, err)
	}
	return got
}

// A loaded variable behaves as it always has. Everything below changes what one
// name does, and this is the case that must not move.
func TestAnOrdinaryVariableIsTheStringItWasLoadedWith(t *testing.T) {
	if got := evalEnv(t, `env.GREETING`, map[string]string{"GREETING": "hello"}); got != "hello" {
		t.Errorf("env.GREETING = %q, want the loaded value", got)
	}
}

// The reason the seam exists: the map is built once and shared across every
// message a block handles, so a value read at that moment would be the same
// string for the life of the process. A credential cannot be.
func TestARegisteredValueIsResolvedEachTimeItIsRead(t *testing.T) {
	var reads int
	RegisterEnvValue("TEST_ROTATING", func() string {
		reads++
		return fmt.Sprintf("token-%d", reads)
	})
	t.Cleanup(func() { envProviders.Delete("TEST_ROTATING") })

	// One activation, built once, exactly as a block holds it.
	activation := EnvActivation(nil)
	first := mustEval(t, `env.TEST_ROTATING`, activation)
	second := mustEval(t, `env.TEST_ROTATING`, activation)

	if first != "token-1" || second != "token-2" {
		t.Errorf("reads gave %q then %q, want a fresh value each time", first, second)
	}
}

// A provider is the authority on the value it maintains, so a stale copy sitting
// in the environment does not win — that copy is the thing it exists to replace.
func TestARegisteredValueBeatsALoadedOneOfTheSameName(t *testing.T) {
	RegisterEnvValue("TEST_SHADOWED", func() string { return "fresh" })
	t.Cleanup(func() { envProviders.Delete("TEST_SHADOWED") })

	if got := evalEnv(t, `env.TEST_SHADOWED`, map[string]string{"TEST_SHADOWED": "stale"}); got != "fresh" {
		t.Errorf("env.TEST_SHADOWED = %q, want the provider's value", got)
	}
}

// A build whose services provider registers nothing does the plain lookup, which
// is what lets one definition load in the editor, run under dolphin, and work in
// the cluster. Unset is a missing key, as it has always been.
func TestWithNoProviderTheNameIsAnOrdinaryLookup(t *testing.T) {
	if got := evalEnv(t, `env.TEST_PLAIN`, map[string]string{"TEST_PLAIN": "set-by-hand"}); got != "set-by-hand" {
		t.Errorf("env.TEST_PLAIN = %q, want the environment's own value", got)
	}
	if _, err := CompileMessage(nil, `env.TEST_ABSENT`); err != nil {
		t.Fatalf("an unregistered name must still compile: %v", err)
	}
	program, _ := CompileMessage(nil, `env.TEST_ABSENT`)
	if _, err := program.EvalString(MessageActivation(&types.Message{}, EnvActivation(nil))); err == nil {
		t.Error("reading an unset variable succeeded; it must stay a missing-key error")
	}
}

// It is a string wherever a string goes, which is the whole point of hiding it
// behind a variable: an author writes the header they would have written anyway.
func TestARegisteredValueConcatenatesLikeAnyString(t *testing.T) {
	RegisterEnvValue("TEST_BEARER", func() string { return "abc" })
	t.Cleanup(func() { envProviders.Delete("TEST_BEARER") })

	if got := evalEnv(t, `"Bearer " + env.TEST_BEARER`, nil); got != "Bearer abc" {
		t.Errorf("concatenation gave %q", got)
	}
}

func mustEval(t *testing.T, expression string, activation Env) string {
	t.Helper()
	program, err := CompileMessage(nil, expression)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := program.EvalString(MessageActivation(&types.Message{}, activation))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return got
}

// `has(env.NAME)` is how a definition asks whether this runtime has the value at
// all — the agent's tools use it to decide between the caller's credential and
// the deployment's own. It has to answer about the provider, not about whether
// somebody also set a variable of that name.
func TestPresenceAnswersAboutTheProvider(t *testing.T) {
	RegisterEnvValue("TEST_PRESENT", func() string { return "here" })
	t.Cleanup(func() { envProviders.Delete("TEST_PRESENT") })

	if got := evalEnv(t, `has(env.TEST_PRESENT) ? env.TEST_PRESENT : "absent"`, nil); got != "here" {
		t.Errorf("with a provider and nothing in the environment, got %q", got)
	}
	if got := evalEnv(t, `has(env.TEST_MISSING) ? "present" : "absent"`, nil); got != "absent" {
		t.Errorf("with no provider and nothing in the environment, got %q", got)
	}
	if got := evalEnv(t, `has(env.TEST_LOADED) ? "present" : "absent"`,
		map[string]string{"TEST_LOADED": "x"}); got != "present" {
		t.Errorf("with an ordinary variable, got %q", got)
	}
}

// It is a string wherever a string goes, which is the whole point of resolving in
// the lookup rather than in the value: the rest of the language never learns this
// happened.
func TestARegisteredValueBehavesLikeAnyOtherString(t *testing.T) {
	RegisterEnvValue("TEST_STRINGY", func() string { return "abc" })
	t.Cleanup(func() { envProviders.Delete("TEST_STRINGY") })

	for _, tc := range []struct{ expression, want string }{
		{`"Bearer " + env.TEST_STRINGY`, "Bearer abc"},
		{`env.TEST_STRINGY.startsWith("a") ? "yes" : "no"`, "yes"},
		{`string(size(env.TEST_STRINGY))`, "3"},
		{`env.TEST_STRINGY == "abc" ? "equal" : "not"`, "equal"},
		{`env.TEST_STRINGY.matches("^a.c$") ? "matched" : "no"`, "matched"},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			if got := evalEnv(t, tc.expression, nil); got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
		})
	}
}
