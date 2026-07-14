package assert

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
	"github.com/juancavallotti/octo/runtime/types"
)

// caseFrom builds a case from the YAML a user would actually write, rather than from Go
// literals. It matters here more than usual: the whole point of normalize() is that YAML
// decodes 42 as an int while the message carries the float64 that JSON decoding produced,
// and a test that built its expectations as Go literals could not see that at all.
func caseFrom(t *testing.T, src string) suite.Case {
	t.Helper()
	var c suite.Case
	decoder := yaml.NewDecoder(strings.NewReader(src))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		t.Fatalf("decode case: %v", err)
	}
	return c
}

// msg builds a message the way octo hands one back: its body decoded from JSON, so every
// number in it is a float64.
func msg(t *testing.T, body string, vars map[string]any) *types.Message {
	t.Helper()
	m, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if body != "" {
		if err := m.SetBodyJSON([]byte(body)); err != nil {
			t.Fatalf("SetBodyJSON: %v", err)
		}
	}
	for name, value := range vars {
		m.Variables.Set(name, value)
	}
	return m
}

// completed is an outcome for a flow that returned msg.
func completed(m *types.Message) runner.Outcome { return runner.Outcome{Result: m} }

// only fails unless Check returned exactly the failures whose text contains each want.
func only(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d failures, want %d:\n  got:  %v\n  want: %v", len(got), len(want), got, want)
	}
	for i, w := range want {
		if !strings.Contains(got[i], w) {
			t.Errorf("failure %d = %q,\nwant it to contain %q", i, got[i], w)
		}
	}
}

// TestNumbersFromYAMLCompareAgainstNumbersFromJSON is the trap this package exists to
// avoid, and the one that would have broken every suite ever written.
//
// The expected body is decoded from YAML, where 42 is an int. The actual body came back
// from octo as JSON, where 42 is a float64. reflect.DeepEqual says an int(42) and a
// float64(42) differ — so without normalizing, this case fails, and the report reads
// `want: 42, got: 42`, which is the worst possible thing to hand someone at 2am.
func TestNumbersFromYAMLCompareAgainstNumbersFromJSON(t *testing.T) {
	c := caseFrom(t, `
name: it compares numbers
expect:
  body: { id: 1, amount: 250, ratio: 0.5, ok: true, tag: gold, items: [1, 2] }
`)
	got := Check(c, completed(msg(t, `{"id":1,"amount":250,"ratio":0.5,"ok":true,"tag":"gold","items":[1,2]}`, nil)))

	if len(got) > 0 {
		t.Errorf("a body that matches was reported as failing — numbers were not normalized:\n  %s",
			strings.Join(got, "\n  "))
	}
}

// TestBodyIsExact: the body is the flow's answer, and a test earns its keep by failing
// when a field appears in it that nobody expected.
func TestBodyIsExact(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  body: { id: 1 }\n")

	got := Check(c, completed(msg(t, `{"id":1,"surprise":"a new field nobody asked for"}`, nil)))

	only(t, got, "expect.body")
	if !strings.Contains(got[0], "surprise") {
		t.Errorf("the failure does not show what actually came back:\n%s", got[0])
	}
}

// TestVarsAreASubset: variables are scratch space the engine and the blocks add to —
// vars.error, vars.httpStatus, whatever a set-variable left behind — so pinning the whole
// map would break a suite on every unrelated change. Only what is named is checked.
func TestVarsAreASubset(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  vars: { httpStatus: \"502\" }\n")

	outcome := completed(msg(t, `{}`, map[string]any{
		"httpStatus": "502",
		"error":      map[string]any{"block": "charge"}, // the engine put this here
		"scratch":    "left behind by a set-variable",
	}))

	if got := Check(c, outcome); len(got) > 0 {
		t.Errorf("an unnamed variable failed the case:\n  %s", strings.Join(got, "\n  "))
	}
}

// TestAMissingVariableSaysWhichOnesAreThere. The commonest cause is a typo, and the list
// answers the question the reader is about to ask.
func TestAMissingVariableSaysWhichOnesAreThere(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  vars: { httpStatis: \"502\" }\n")

	got := Check(c, completed(msg(t, `{}`, map[string]any{"httpStatus": "502", "tier": "gold"})))

	only(t, got, "expect.vars.httpStatis: not set")
	if !strings.Contains(got[0], "httpStatus, tier") {
		t.Errorf("the failure does not list the variables that ARE set:\n%s", got[0])
	}
}

// TestThatIsCEL, over the same variables every block in the flow uses.
func TestThatIsCEL(t *testing.T) {
	c := caseFrom(t, `
name: a
expect:
  that:
    - 'body.amount > 100'
    - 'vars.tier == "gold"'
    - 'body.items.size() == 2'
    - '!has(body.refund)'
`)
	outcome := completed(msg(t, `{"amount":250,"items":[1,2]}`, map[string]any{"tier": "gold"}))

	if got := Check(c, outcome); len(got) > 0 {
		t.Errorf("a true expression was reported as false:\n  %s", strings.Join(got, "\n  "))
	}
}

// TestAFalseExpressionFailsAndSaysWhichOne.
func TestAFalseExpressionFailsAndSaysWhichOne(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  that: ['body.amount > 100', 'body.amount < 10']\n")

	got := Check(c, completed(msg(t, `{"amount":250}`, nil)))

	only(t, got, "expect.that: body.amount < 10 is false")
}

// TestANonBooleanExpressionIsAMistake, not a falsy value.
//
// `body.total` is not an assertion. Reading it as one — truthy because it is non-zero —
// would pass a case that checks nothing, which is the failure this whole package exists
// to prevent. So it is rejected, loudly, rather than coerced.
func TestANonBooleanExpressionIsAMistake(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  that: ['body.amount']\n")

	got := Check(c, completed(msg(t, `{"amount":250}`, nil)))

	only(t, got, "is not a true/false expression")
}

// TestABrokenExpressionIsReportedAsBroken: an expression that does not compile must not
// be silently skipped — that would be a suite asserting nothing and going green.
func TestABrokenExpressionIsReportedAsBroken(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  that: ['body.amount >>> 100']\n")

	got := Check(c, completed(msg(t, `{"amount":250}`, nil)))

	only(t, got, "does not compile")
}

// TestEnvIsNotBound is an honest limit of driving octo as a subprocess: dolphin never
// loads the config — octo does, in the child — so there is no resolved environment to
// activate. An expression that reaches for it must SAY so, not quietly evaluate to
// nothing.
func TestEnvIsNotBound(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  that: ['env.API_KEY == \"secret\"']\n")

	got := Check(c, completed(msg(t, `{}`, nil)))

	only(t, got, "did not evaluate")
}

// TestWhatTheFlowDidComesFirst. A flow that failed has no message, so checking its body
// would produce six complaints that it is missing — noise burying the one line that
// matters. The outcome short-circuits.
func TestWhatTheFlowDidComesFirst(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  body: { ok: true }\n  vars: { tier: gold }\n  that: ['body.ok']\n")

	got := Check(c, runner.Outcome{Error: `block "charge": card declined`})

	only(t, got, "the flow failed: block \"charge\": card declined")
}

// TestAnExpectedFailureMustMatch: a case expecting "card declined" that passes on ANY
// failure is a test that proves the wrong thing, which is worse than no test.
func TestAnExpectedFailureMustMatch(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  error: card declined\n")

	t.Run("the right failure passes", func(t *testing.T) {
		if got := Check(c, runner.Outcome{Error: `block "charge": card declined`}); len(got) > 0 {
			t.Errorf("the expected failure was reported as a failure: %v", got)
		}
	})

	t.Run("a different failure does not", func(t *testing.T) {
		got := Check(c, runner.Outcome{Error: `block "charge": connection refused`})
		only(t, got, "expect.error")
	})

	t.Run("a flow that did not fail at all does not", func(t *testing.T) {
		got := Check(c, completed(msg(t, `{}`, nil)))
		only(t, got, "the flow did not fail, it completed")
	})
}

// TestAnExpectedDrop.
func TestAnExpectedDrop(t *testing.T) {
	c := caseFrom(t, "name: a\nexpect:\n  dropped: true\n")

	if got := Check(c, runner.Outcome{Dropped: true}); len(got) > 0 {
		t.Errorf("an expected drop was reported as a failure: %v", got)
	}

	got := Check(c, completed(msg(t, `{}`, nil)))
	only(t, got, "the flow did not drop the message, it completed")
}

// TestASmokeTestStillAssertsSomething: a case with no `expect` at all asserts that the
// flow completed. A smoke test that goes green when the flow blew up is not a test.
func TestASmokeTestStillAssertsSomething(t *testing.T) {
	c := caseFrom(t, "name: a\n")

	if got := Check(c, completed(msg(t, `{}`, nil))); len(got) > 0 {
		t.Errorf("a flow that completed failed a case that asserts nothing else: %v", got)
	}

	only(t, Check(c, runner.Outcome{Error: "boom"}), "the flow failed: boom")
	only(t, Check(c, runner.Outcome{Dropped: true}), "the flow dropped the message")
}

// TestEveryFailureIsReported, not just the first. A case that got the wrong body has
// probably also got the wrong variables, and telling the user about one, then about the
// next after they fix it, then about the third, turns one debugging session into three.
func TestEveryFailureIsReported(t *testing.T) {
	c := caseFrom(t, `
name: a
expect:
  body: { id: 2 }
  vars: { tier: platinum }
  that: ['body.id == 2']
`)
	got := Check(c, completed(msg(t, `{"id":1}`, map[string]any{"tier": "gold"})))

	only(t, got, "expect.body", "expect.vars.tier", "expect.that")
}
