package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stdinReadTimeout bounds how long a call under withPipedStdin may sit on stdin before
// the test calls it blocked. Nothing here does real work, so anything near this is a
// read that should not have happened.
const stdinReadTimeout = 2 * time.Second

// withPipedStdin runs fn with os.Stdin replaced by a pipe carrying content, as a shell
// redirect would. An empty content leaves the pipe OPEN with nothing in it — the state
// a script's inherited stdin is in, and the one that makes a read block forever.
//
// fn runs on its own goroutine so that a blocked read fails the test with a message
// rather than hanging the suite until the package timeout fires.
func withPipedStdin(t *testing.T, content string, fn func() ([]byte, error)) ([]byte, error) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = read.Close() }()

	orig := os.Stdin
	os.Stdin = read
	defer func() { os.Stdin = orig }()

	if content != "" {
		if _, err := write.WriteString(content); err != nil {
			t.Fatalf("write stdin: %v", err)
		}
		// Closed, so a reader sees EOF and returns. Left open when there is nothing to
		// send, so a reader blocks — see above.
		if err := write.Close(); err != nil {
			t.Fatalf("close stdin writer: %v", err)
		}
	} else {
		defer func() { _ = write.Close() }()
	}

	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		body, fnErr := fn()
		done <- result{body: body, err: fnErr}
	}()

	select {
	case got := <-done:
		return got.body, got.err
	case <-time.After(stdinReadTimeout):
		t.Fatal("blocked reading stdin: a run that already has a body must not wait on a pipe")
		return nil, nil
	}
}

// writeConfig writes a debug config to a temp file and returns its path.
func writeConfig(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// theSameRunInYAMLAndJSON is one debug session written both ways. JSON is a subset of
// YAML, so the loader takes both for free — which is what lets a --mocks blob that
// worked on the command line be pasted straight into a file.
const (
	sessionYAML = `
input:
  data: { amount: 250 }
  vars: { x-api-key: dev-key }
breakAt: orders.charge
spies:
  - orders.validate
  - orders.fanout[audit].log-it
mocks:
  orders.charge:
    cases:
      - when: 'body.amount > 100'
        error: card declined
    default:
      drop: true
`
	sessionJSON = `{
  "input": {"data": {"amount": 250}, "vars": {"x-api-key": "dev-key"}},
  "breakAt": "orders.charge",
  "spies": ["orders.validate", "orders.fanout[audit].log-it"],
  "mocks": {
    "orders.charge": {
      "cases": [{"when": "body.amount > 100", "error": "card declined"}],
      "default": {"drop": true}
    }
  }
}`
)

func TestLoadDebugConfigTakesYAMLAndJSON(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
	}{
		{name: "yaml", file: "debug.yaml", content: sessionYAML},
		{name: "json", file: "debug.json", content: sessionJSON},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadDebugConfig(writeConfig(t, tc.file, tc.content))
			if err != nil {
				t.Fatalf("loadDebugConfig: %v", err)
			}

			if cfg.BreakAt != "orders.charge" {
				t.Errorf("breakAt = %q, want %q", cfg.BreakAt, "orders.charge")
			}
			if len(cfg.Spies) != 2 || cfg.Spies[1] != "orders.fanout[audit].log-it" {
				t.Errorf("spies = %v, want both addresses in order", cfg.Spies)
			}

			spec, ok := cfg.Mocks["orders.charge"]
			if !ok {
				t.Fatal("no mock spec for orders.charge")
			}
			if len(spec.Cases) != 1 || spec.Cases[0].Error != "card declined" {
				t.Errorf("cases = %+v, want the declining case", spec.Cases)
			}
			if spec.Default == nil || !spec.Default.Drop {
				t.Errorf("default = %+v, want a drop", spec.Default)
			}

			// input.data is written as a structured value and re-encoded to JSON on the
			// way to the message.
			body, err := cfg.body()
			if err != nil {
				t.Fatalf("body: %v", err)
			}
			if string(body) != `{"amount":250}` {
				t.Errorf("body = %s, want the input.data re-encoded as JSON", body)
			}
			if key, _ := cfg.Input.Vars.String("x-api-key"); key != "dev-key" {
				t.Errorf("vars = %v, want the seeded api key", cfg.Input.Vars)
			}
		})
	}
}

// TestLoadDebugConfigRejectsUnknownKeys: a debug config is hand-written, and a typo'd
// key that is silently ignored looks exactly like a feature that does not work — a
// misspelled `spys:` would report no records, and the user would go hunting for the
// bug in their flow.
func TestLoadDebugConfigRejectsUnknownKeys(t *testing.T) {
	path := writeConfig(t, "typo.yaml", "spys:\n  - orders.charge\n")

	_, err := loadDebugConfig(path)
	if err == nil {
		t.Fatal("an unknown key must be rejected")
	}
	if !strings.Contains(err.Error(), "spys") {
		t.Errorf("error = %q, want it to name the key it did not recognize", err)
	}
}

func TestLoadDebugConfigEmptyPathIsTheZeroConfig(t *testing.T) {
	cfg, err := loadDebugConfig("")
	if err != nil {
		t.Fatalf("loadDebugConfig(\"\"): %v", err)
	}

	spies, err := cfg.spies()
	if err != nil || spies != nil {
		t.Errorf("spies = %v, %v; want no collector and no error", spies, err)
	}
	mocks, err := cfg.mocks()
	if err != nil || mocks != nil {
		t.Errorf("mocks = %v, %v; want no mocks and no error", mocks, err)
	}
	body, err := cfg.body()
	if err != nil || body != nil {
		t.Errorf("body = %v, %v; want no body and no error", body, err)
	}
}

func TestLoadDebugConfigMissingFile(t *testing.T) {
	if _, err := loadDebugConfig(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("a missing debug config must be reported, not ignored")
	}
}

// TestFlagsOverrideTheDebugConfigSection pins the precedence rule: a flag replaces the
// whole section it corresponds to, rather than merging into it. Merging a --spies list
// into the file's would leave no way to run the file with FEWER spies than it lists.
func TestFlagsOverrideTheDebugConfigSection(t *testing.T) {
	debug, err := loadDebugConfig(writeConfig(t, "debug.yaml", sessionYAML))
	if err != nil {
		t.Fatalf("loadDebugConfig: %v", err)
	}

	t.Run("--spies replaces the list", func(t *testing.T) {
		spies, err := resolveSpies("orders.other", debug)
		if err != nil {
			t.Fatalf("resolveSpies: %v", err)
		}
		got := spies.Addresses()
		if len(got) != 1 || got[0] != "orders.other" {
			t.Errorf("spies = %v, want only the flag's address — not merged with the file's", got)
		}
	})

	t.Run("the file's list is used when the flag is absent", func(t *testing.T) {
		spies, err := resolveSpies("", debug)
		if err != nil {
			t.Fatalf("resolveSpies: %v", err)
		}
		if len(spies.Addresses()) != 2 {
			t.Errorf("spies = %v, want the file's two addresses", spies.Addresses())
		}
	})

	t.Run("--mocks replaces the specs", func(t *testing.T) {
		mocks, err := resolveMocks(`{"orders.other":{"cases":[{"when":"true","drop":true}]}}`, debug)
		if err != nil {
			t.Fatalf("resolveMocks: %v", err)
		}
		if _, ok := mocks.For("orders.charge"); ok {
			t.Error("the file's mock survived a --mocks flag: the flag must replace the section")
		}
		if _, ok := mocks.For("orders.other"); !ok {
			t.Error("the flag's mock is missing")
		}
	})

	t.Run("--break-at replaces breakAt", func(t *testing.T) {
		if got := override("orders.flagged", debug.BreakAt); got != "orders.flagged" {
			t.Errorf("breakAt = %q, want the flag's address", got)
		}
		if got := override("", debug.BreakAt); got != "orders.charge" {
			t.Errorf("breakAt = %q, want the file's address when no flag was given", got)
		}
	})

	t.Run("--data beats input.data", func(t *testing.T) {
		body, err := resolveBody(`{"amount":1}`, debug)
		if err != nil {
			t.Fatalf("resolveBody: %v", err)
		}
		if string(body) != `{"amount":1}` {
			t.Errorf("body = %s, want the flag's data", body)
		}
	})

	// The file's body is used without ever consulting stdin. That ordering is the
	// point: `invoke` with no --data waits on stdin, so a run fully described in a
	// file would otherwise block forever in any script that inherits an open pipe,
	// waiting for a body it already has.
	t.Run("input.data is used, and stdin is not consulted", func(t *testing.T) {
		body, err := withPipedStdin(t, "", func() ([]byte, error) { return resolveBody("", debug) })
		if err != nil {
			t.Fatalf("resolveBody: %v", err)
		}
		if string(body) != `{"amount":250}` {
			t.Errorf("body = %s, want the file's input.data", body)
		}
	})

	// A config that carries no input.data still falls through to stdin, so piping into
	// a config that only lists spies and mocks works as it reads.
	t.Run("a config with no input.data falls through to stdin", func(t *testing.T) {
		spiesOnly, err := loadDebugConfig(writeConfig(t, "spies.yaml", "spies:\n  - orders.charge\n"))
		if err != nil {
			t.Fatalf("loadDebugConfig: %v", err)
		}

		body, err := withPipedStdin(t, `{"amount":7}`, func() ([]byte, error) {
			return resolveBody("", spiesOnly)
		})
		if err != nil {
			t.Fatalf("resolveBody: %v", err)
		}
		if string(body) != `{"amount":7}` {
			t.Errorf("body = %s, want what was piped in", body)
		}
	})

	t.Run("--vars replaces input.vars", func(t *testing.T) {
		vars, err := resolveVars(`{"tier":"vip"}`, debug)
		if err != nil {
			t.Fatalf("resolveVars: %v", err)
		}
		if tier, _ := vars.String("tier"); tier != "vip" {
			t.Errorf("vars = %v, want the flag's", vars)
		}
		if _, present := vars["x-api-key"]; present {
			t.Error("the file's vars survived a --vars flag: the flag must replace the section")
		}
	})
}

// TestDebugConfigValidatesLikeTheFlags: a bad spec in the file is caught on load, with
// the same message the flag would give — the file is not a way to smuggle in a spec the
// flag would have rejected.
func TestDebugConfigValidatesLikeTheFlags(t *testing.T) {
	debug, err := loadDebugConfig(writeConfig(t, "bad.yaml",
		"mocks:\n  orders.charge:\n    cases:\n      - when: 'true'\n"))
	if err != nil {
		t.Fatalf("loadDebugConfig: %v", err)
	}

	if _, err := debug.mocks(); err == nil {
		t.Fatal("a case with no outcome must be rejected")
	} else if !strings.Contains(err.Error(), "needs an outcome") {
		t.Errorf("error = %q, want it to say what is wrong with the spec", err)
	}
}
