package suite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// write puts content in a temp file and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// load reads a suite, failing the test if it does not load.
func load(t *testing.T, content string) File {
	t.Helper()
	file, err := Load(write(t, "orders_test.yaml", content))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return file
}

// loadErr reads a suite that must not load, and returns why.
func loadErr(t *testing.T, content string) string {
	t.Helper()
	_, err := Load(write(t, "orders_test.yaml", content))
	if err == nil {
		t.Fatal("the suite loaded, but it should have been rejected")
	}
	return err.Error()
}

// suiteYAML exercises every section of the format at once. It is the documented
// example, and it must stay one the loader takes.
const suiteYAML = `
flow: charge-flowlevel

inputs:
  small:
    data: { amount: 10 }
  large:
    data: { amount: 500 }
    vars: { x-api-key: dev-key }

mocks:
  charge-flowlevel.call-charge:
    cases:
      - when: 'body.amount > 100'
        error: card declined
    default:
      body: { id: ch_1, status: captured }

timeout: 10s

cases:
  - name: a small charge is captured
    input: small
    expect:
      body: { id: ch_1, status: captured }

  - name: a declined card takes the error path
    input: large
    timeout: 5s
    expect:
      vars: { httpStatus: "502" }
      that:
        - 'body.error.contains("declined")'
    spies:
      charge-flowlevel.call-charge:
        count: 1
        records:
          - input:
              that: ['body.amount == 500']
            error: card declined

  - name: it is filtered out
    input:
      data: { amount: 0 }
    mocks:
      charge-flowlevel.call-charge:
        cases:
          - when: 'true'
            drop: true
    expect:
      dropped: true

  - name: not ready yet
    skip: the upstream contract is still moving
`

func TestLoadTestFileReadsEverySection(t *testing.T) {
	file := load(t, suiteYAML)

	if file.Flow != "charge-flowlevel" {
		t.Errorf("flow = %q", file.Flow)
	}
	if len(file.Cases) != 4 {
		t.Fatalf("loaded %d cases, want 4", len(file.Cases))
	}
	if len(file.Inputs) != 2 {
		t.Errorf("loaded %d inputs, want 2", len(file.Inputs))
	}
	if file.Cases[3].Skip == "" {
		t.Error("the skip reason was dropped")
	}
	// The spy's count is a pointer, so that `count: 0` can mean "never ran".
	spy := file.Cases[1].Spies["charge-flowlevel.call-charge"]
	if spy.Count == nil || *spy.Count != 1 {
		t.Errorf("spy count = %v, want 1", spy.Count)
	}
	if len(spy.Records) != 1 || spy.Records[0].Error != "card declined" {
		t.Errorf("spy records = %+v", spy.Records)
	}
}

// TestBlockAddressesSurviveAsYAMLKeys: an address carries brackets —
// demo.each-order[body].classify-order — and it is a mapping KEY in both `spies:` and
// `mocks:`. YAML only forbids those characters inside a plain scalar in flow context,
// so a block-style key needs no quoting; this pins that, because the day it stops
// being true every suite that spies inside a composite breaks at once.
func TestBlockAddressesSurviveAsYAMLKeys(t *testing.T) {
	const nested = `
flow: demo
cases:
  - name: it classifies each order
    spies:
      demo.each-order[body].classify-order:
        count: 2
      demo[error].notify:
        count: 0
    mocks:
      demo.any-orders[else].log:
        cases:
          - when: 'true'
            drop: true
`
	file := load(t, nested)
	c := file.Cases[0]

	for _, address := range []string{"demo.each-order[body].classify-order", "demo[error].notify"} {
		if _, ok := c.Spies[address]; !ok {
			t.Errorf("the spy address %q did not survive as a YAML key: %v", address, SpiedBy(c))
		}
	}
	if _, ok := c.Mocks["demo.any-orders[else].log"]; !ok {
		t.Errorf("the mock address did not survive as a YAML key: %v", c.Mocks)
	}
	// count: 0 is an assertion — "this block never ran" — not the absence of one.
	never := c.Spies["demo[error].notify"]
	if never.Count == nil || *never.Count != 0 {
		t.Errorf("count = %v, want a set 0: the error path must be assertable as never taken", never.Count)
	}
}

// TestInputIsANameOrAnInlineOne: both are common — a body six cases share wants a
// name, a body used once does not — so `input:` takes either.
func TestInputIsANameOrAnInlineOne(t *testing.T) {
	file := load(t, suiteYAML)

	named := file.InputFor(file.Cases[0])
	body, ok := named.Data.(map[string]any)
	if !ok || body["amount"] != 10 {
		t.Errorf("named input resolved to %v, want the shared `small`", named.Data)
	}

	inline := file.InputFor(file.Cases[2])
	body, ok = inline.Data.(map[string]any)
	if !ok || body["amount"] != 0 {
		t.Errorf("inline input resolved to %v", inline.Data)
	}

	// A case with no input at all calls the flow with an empty message, which is a
	// legitimate thing to test.
	if got := file.InputFor(file.Cases[3]); got.Data != nil {
		t.Errorf("a case with no input got %v, want an empty message", got.Data)
	}
}

// TestCaseMocksOverrideTheFilesPerAddress: a case's mock replaces the file's for that
// address, whole. Merging case-by-case would mean a reader had to hold the file's
// version in their head to know what the case's block will do.
func TestCaseMocksOverrideTheFilesPerAddress(t *testing.T) {
	const twoAddresses = `
flow: orders
mocks:
  orders.charge:
    cases: [{ when: 'true', body: { id: from-file } }]
  orders.notify:
    cases: [{ when: 'true', drop: true }]
cases:
  - name: it overrides one and inherits the other
    mocks:
      orders.charge:
        cases: [{ when: 'true', error: from the case }]
`
	file := load(t, twoAddresses)
	merged := file.MocksFor(file.Cases[0])

	if got := merged["orders.charge"].Cases[0].Error; got != "from the case" {
		t.Errorf("orders.charge = %q, want the case's mock to have replaced the file's", got)
	}
	if got := merged["orders.charge"].Cases[0].Body; got != nil {
		t.Errorf("orders.charge kept the file's body %v — the override must be whole-spec", got)
	}
	if _, ok := merged["orders.notify"]; !ok {
		t.Error("orders.notify was lost: an address the case says nothing about keeps the file's mock")
	}
}

// TestTimeoutFallsBackFromCaseToFileToDefault.
func TestTimeoutFallsBackFromCaseToFileToDefault(t *testing.T) {
	file := load(t, suiteYAML)

	if got := file.TimeoutFor(file.Cases[1]); got != 5*time.Second {
		t.Errorf("case timeout = %v, want its own 5s", got)
	}
	if got := file.TimeoutFor(file.Cases[0]); got != 10*time.Second {
		t.Errorf("case timeout = %v, want the file's 10s", got)
	}
	if got := (File{}).TimeoutFor(Case{}); got != DefaultCaseTimeout {
		t.Errorf("case timeout = %v, want the %v default", got, DefaultCaseTimeout)
	}
}

// TestLoadTestFileRejects is the heart of it. A test file is hand-written, and every
// mistake here is one that would otherwise produce a suite that passes while asserting
// nothing — which is worse than having no suite at all. Each of these must be caught
// before a single octo is spawned.
func TestLoadTestFileRejects(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string // a substring of the error
	}{{
		// The one that motivates KnownFields: a misspelled `spys:` would watch nothing,
		// assert nothing, and go green.
		name:    "a typo'd key",
		content: "flow: orders\ncases:\n  - name: a\n    spys:\n      orders.charge: {}\n",
		want:    "field spys not found",
	}, {
		// A custom unmarshaller does not inherit KnownFields, so the inline input is the
		// one place a typo could still slip through. knownKeys is why it does not.
		name:    "a typo'd key inside an inline input",
		content: "flow: orders\ncases:\n  - name: a\n    input:\n      daat: { x: 1 }\n",
		want:    "field daat not found",
	}, {
		name:    "no flow",
		content: "cases:\n  - name: a\n",
		want:    "needs a `flow`",
	}, {
		name:    "no cases",
		content: "flow: orders\n",
		want:    "has no `cases`",
	}, {
		name:    "an unnamed case",
		content: "flow: orders\ncases:\n  - expect: { dropped: true }\n",
		want:    "needs a `name`",
	}, {
		name:    "two cases with the same name",
		content: "flow: orders\ncases:\n  - name: a\n  - name: a\n",
		want:    `two cases are named "a"`,
	}, {
		// A name that does not resolve would otherwise call the flow with an empty
		// message and assert against it — a test that runs, and tests the wrong thing.
		name:    "an input that is not declared",
		content: "flow: orders\ninputs:\n  small: { data: {} }\ncases:\n  - name: a\n    input: larg\n",
		want:    `input "larg" is not declared`,
	}, {
		name:    "an input named when none are declared",
		content: "flow: orders\ncases:\n  - name: a\n    input: small\n",
		want:    "have: none",
	}, {
		// A flow that failed returned no message, so there is no body to compare against:
		// this case could never pass, and saying so beats failing it on every run.
		name:    "a body expected alongside a failure",
		content: "flow: orders\ncases:\n  - name: a\n    expect:\n      error: boom\n      body: { ok: true }\n",
		want:    "a flow that failed returned none",
	}, {
		name:    "a body expected alongside a drop",
		content: "flow: orders\ncases:\n  - name: a\n    expect:\n      dropped: true\n      that: ['body.ok']\n",
		want:    "a flow that dropped the message returned none",
	}, {
		name:    "both dropped and error",
		content: "flow: orders\ncases:\n  - name: a\n    expect:\n      dropped: true\n      error: boom\n",
		want:    "did not filter the message out",
	}, {
		name: "a spied record with two outcomes",
		content: "flow: orders\ncases:\n  - name: a\n    spies:\n      orders.charge:\n" +
			"        records:\n          - error: boom\n            output: { body: { ok: true } }\n",
		want: "expects more than one outcome",
	}, {
		name: "more records asserted than the count allows",
		content: "flow: orders\ncases:\n  - name: a\n    spies:\n      orders.charge:\n" +
			"        count: 1\n        records:\n          - {}\n          - {}\n",
		want: "asserts on 2 records but expects a count of 1",
	}, {
		// The engine's own mock validation, run here: a bad spec is the suite's mistake,
		// and it should be reported by dolphin now rather than by an octo three cases later.
		name:    "a mock case with no outcome",
		content: "flow: orders\nmocks:\n  orders.charge:\n    cases: [{ when: 'true' }]\ncases:\n  - name: a\n",
		want:    "needs an outcome",
	}, {
		name: "a mock case with two outcomes",
		content: "flow: orders\ncases:\n  - name: a\n    mocks:\n      orders.charge:\n" +
			"        cases: [{ when: 'true', drop: true, error: boom }]\n",
		want: "more than one outcome",
	}, {
		name:    "an input that is neither a name nor a mapping",
		content: "flow: orders\ncases:\n  - name: a\n    input: [1, 2]\n",
		want:    "the name of a shared input, or an inline",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := loadErr(t, tc.content)
			if !strings.Contains(got, tc.want) {
				t.Errorf("error = %q,\nwant it to contain %q", got, tc.want)
			}
		})
	}
}

// TestRejectionsNameTheFile: every rejection is reported against the file it came
// from. A suite is usually one of several in a run, and "case \"a\": needs a name" with
// no file in front of it sends the reader to the wrong one.
//
// The rejection is a plain error, deliberately: it carries no exit code, because a
// package that describes a file format has no business knowing about process exit
// codes. dolphin's exitCode maps an unclassified error to exitUsage, so a malformed
// suite still exits 2 rather than 1 — which matters, because 1 means "a test failed",
// and a CI job that cannot tell "your suite is broken" from "your flow is broken"
// sends someone to debug the wrong thing.
func TestRejectionsNameTheFile(t *testing.T) {
	path := write(t, "orders_test.yaml", "flow: orders\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("a suite with no cases must be rejected")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name %s", err, path)
	}
}

// TestSpiedByIsSorted: the addresses go to octo on a command line, and two runs of the
// same suite must spawn it with identical arguments — otherwise the reproduce command
// a failure prints is not the command that failed.
func TestSpiedByIsSorted(t *testing.T) {
	c := Case{Spies: map[string]SpyExpect{
		"orders.zed":    {},
		"orders.alpha":  {},
		"orders.middle": {},
	}}

	got := SpiedBy(c)
	want := []string{"orders.alpha", "orders.middle", "orders.zed"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("spiedBy = %v, want %v", got, want)
	}
}
