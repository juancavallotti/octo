package suite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalSuite is a suite that loads. Discovery does not care what is in it.
const minimalSuite = "flow: orders\ncases:\n  - name: a\n"

const minimalFlow = "flows:\n  - name: orders\n    process: []\n"

// tree writes files into a fresh directory and returns it. Keys are names, values
// contents.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// pairs renders the discovered targets as "config -> suite", relative to dir, so a test
// can assert on the pairing in one line.
func pairs(t *testing.T, dir string, targets []Target) []string {
	t.Helper()
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		config, err := filepath.Rel(dir, target.Config)
		if err != nil {
			config = target.Config
		}
		path, err := filepath.Rel(dir, target.File.Path)
		if err != nil {
			path = target.File.Path
		}
		out = append(out, config+" -> "+path)
	}
	return out
}

// TestDiscoverTakesAPathAsItIsMeant is the whole of the pairing rule: the config is
// what you point at. A directory is a config the way `octo --config <dir>` means it; a
// flow file is a config the way `octo --config <file>` means it. Nothing is inferred
// beyond the companion naming, so what dolphin hands octo is always something the user
// could have typed.
func TestDiscoverTakesAPathAsItIsMeant(t *testing.T) {
	dir := tree(t, map[string]string{
		"orders.yaml":      minimalFlow,
		"orders_test.yaml": minimalSuite,
	})

	tests := []struct {
		name string
		args []string
		want string
	}{{
		name: "a directory is the config, and holds the suites",
		args: []string{dir},
		want: ". -> orders_test.yaml",
	}, {
		name: "a flow file is the config, and names its companion",
		args: []string{filepath.Join(dir, "orders.yaml")},
		want: "orders.yaml -> orders_test.yaml",
	}, {
		name: "a suite names itself, and runs against the flow file beside it",
		args: []string{filepath.Join(dir, "orders_test.yaml")},
		want: "orders.yaml -> orders_test.yaml",
	}, {
		// Both spellings resolve to the same pair, so naming them together must run the
		// suite once — not twice, and not "ok, 2 files" for one file.
		name: "a flow file and its own suite together are one target",
		args: []string{filepath.Join(dir, "orders.yaml"), filepath.Join(dir, "orders_test.yaml")},
		want: "orders.yaml -> orders_test.yaml",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			targets, err := Discover(tc.args, "")
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			got := pairs(t, dir, targets)
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("discovered %v, want [%s]", got, tc.want)
			}
		})
	}
}

// TestDiscoverFindsEverySuiteInADirectory, sorted, so a run reports its files in the
// same order every time.
func TestDiscoverFindsEverySuiteInADirectory(t *testing.T) {
	dir := tree(t, map[string]string{
		"zeta.yaml":      minimalFlow,
		"zeta_test.yaml": minimalSuite,
		"alpha.yaml":     minimalFlow,
		"alpha_test.yml": minimalSuite, // .yml counts
		"notes.md":       "not a suite",
		// A flow file with no suite is not an error when a DIRECTORY was named — you
		// asked for the suites in it, and there is no suite for this one. It is only an
		// error when the flow file itself was named.
		"untested.yaml": minimalFlow,
	})

	targets, err := Discover([]string{dir}, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	got := strings.Join(pairs(t, dir, targets), " | ")
	want := ". -> alpha_test.yml | . -> zeta_test.yaml"
	if got != want {
		t.Errorf("discovered %q,\nwant       %q", got, want)
	}
}

// TestConfigOverrideSaysWhichFlowsToTest: a suite that lives apart from the flows it
// exercises names them with --config, and that is also how "this flow file, that test
// file" is said without ambiguity.
func TestConfigOverrideSaysWhichFlowsToTest(t *testing.T) {
	dir := tree(t, map[string]string{
		"orders.yaml":     minimalFlow,
		"smoke_test.yaml": minimalSuite, // deliberately NOT orders_test.yaml
	})

	targets, err := Discover([]string{filepath.Join(dir, "smoke_test.yaml")}, filepath.Join(dir, "orders.yaml"))
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	got := pairs(t, dir, targets)
	if len(got) != 1 || got[0] != "orders.yaml -> smoke_test.yaml" {
		t.Errorf("discovered %v, want [orders.yaml -> smoke_test.yaml]", got)
	}
}

// TestSuiteWithNoFlowFileFallsBackToItsDirectory: a suite covering flows split across
// several files has no single companion, so the directory is its config — which is what
// `octo --config <dir>` would load anyway.
func TestSuiteWithNoFlowFileFallsBackToItsDirectory(t *testing.T) {
	dir := tree(t, map[string]string{
		"connectors.yaml": "connectors: []\n",
		"orders.yaml":     minimalFlow,
		"smoke_test.yaml": minimalSuite,
	})

	targets, err := Discover([]string{filepath.Join(dir, "smoke_test.yaml")}, "")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := pairs(t, dir, targets); len(got) != 1 || got[0] != ". -> smoke_test.yaml" {
		t.Errorf("discovered %v, want the directory as the config", got)
	}
}

// TestDiscoverRejects. Each of these would otherwise be a run that reports success
// while having tested nothing, which is the failure mode a test runner must never have.
func TestDiscoverRejects(t *testing.T) {
	withSuite := tree(t, map[string]string{"orders.yaml": minimalFlow, "orders_test.yaml": minimalSuite})
	noSuite := tree(t, map[string]string{"orders.yaml": minimalFlow})

	tests := []struct {
		name   string
		args   []string
		config string
		want   string
	}{{
		// `dolphin test orders.yaml` asked for orders to be tested. Reporting "ok, 0
		// cases" would be a green run that tested nothing.
		name: "a flow file with no companion suite",
		args: []string{filepath.Join(noSuite, "orders.yaml")},
		want: "no test file for",
	}, {
		name: "a directory with no suites in it",
		args: []string{noSuite},
		want: "no *_test.yaml files",
	}, {
		name: "a path that is not there",
		args: []string{filepath.Join(noSuite, "nope.yaml")},
		want: "read",
	}, {
		name:   "a --config that is not there",
		args:   []string{withSuite},
		config: filepath.Join(noSuite, "nope.yaml"),
		want:   "read config",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Discover(tc.args, tc.config)
			if err == nil {
				t.Fatal("discovery succeeded, but it should have been rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestDiscoverValidatesEverySuiteUpFront: a malformed suite stops the run before it
// starts, rather than failing in the middle of it with half the report already printed
// and half the cases already spawned.
func TestDiscoverValidatesEverySuiteUpFront(t *testing.T) {
	dir := tree(t, map[string]string{
		"ok.yaml":       minimalFlow,
		"ok_test.yaml":  minimalSuite,
		"bad.yaml":      minimalFlow,
		"bad_test.yaml": "flow: orders\ncases:\n  - name: a\n    spys: {}\n",
	})

	_, err := Discover([]string{dir}, "")
	if err == nil {
		t.Fatal("a malformed suite in the directory must stop the run")
	}
	if !strings.Contains(err.Error(), "field spys not found") {
		t.Errorf("error = %q, want the malformed suite's own complaint", err)
	}
}
