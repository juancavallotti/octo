package devrun

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// envList parses a rendered definition and returns its resources.env entries, which
// is the only thing injection is allowed to change.
func envList(t *testing.T, definition string) []string {
	t.Helper()
	var doc struct {
		Resources struct {
			Env []string `yaml:"env"`
		} `yaml:"resources"`
	}
	if err := yaml.Unmarshal([]byte(definition), &doc); err != nil {
		t.Fatalf("rendered definition does not parse: %v\n%s", err, definition)
	}
	return doc.Resources.Env
}

func TestInjectDevEnvResourceAppendsLast(t *testing.T) {
	got := injectDevEnvResource("service:\n  name: orders\nresources:\n  env:\n    - .env.shared\n")
	want := []string{".env.shared", devEnvResource}
	if env := envList(t, got); !equalStrings(env, want) {
		t.Fatalf("resources.env = %v, want %v (dev env last so it overrides)", env, want)
	}
}

func TestInjectDevEnvResourceCreatesTheDeclaration(t *testing.T) {
	got := injectDevEnvResource("service:\n  name: orders\n")
	if env := envList(t, got); !equalStrings(env, []string{devEnvResource}) {
		t.Fatalf("resources.env = %v, want just %q", env, devEnvResource)
	}
}

// TestInjectDevEnvResourceFillsANullSequence covers the one YAML shape a hand-written
// definition reaches by accident: `env:` with nothing under it, which parses as null
// rather than as an empty list.
func TestInjectDevEnvResourceFillsANullSequence(t *testing.T) {
	got := injectDevEnvResource("service:\n  name: orders\nresources:\n  env:\n")
	if env := envList(t, got); !equalStrings(env, []string{devEnvResource}) {
		t.Fatalf("resources.env = %v, want just %q", env, devEnvResource)
	}
}

func TestInjectDevEnvResourceIsIdempotent(t *testing.T) {
	once := injectDevEnvResource("resources:\n  env:\n    - .env.dev\n")
	if env := envList(t, once); !equalStrings(env, []string{devEnvResource}) {
		t.Fatalf("resources.env = %v, want just %q; an existing declaration must not be duplicated", env, devEnvResource)
	}
	if twice := injectDevEnvResource(once); twice != once {
		t.Fatalf("second injection changed the definition:\n%s\n---\n%s", once, twice)
	}
}

// TestInjectDevEnvResourceKeepsComments is why this edits the node tree rather than
// round-tripping through a map: a definition the developer cannot recognise in a log
// or an error message is a definition they cannot debug.
func TestInjectDevEnvResourceKeepsComments(t *testing.T) {
	got := injectDevEnvResource("# the orders service\nservice:\n  name: orders # inline\nresources:\n  env:\n    - .env.shared\n")
	for _, want := range []string{"# the orders service", "# inline"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered definition dropped %q:\n%s", want, got)
		}
	}
}

// TestInjectDevEnvResourceKeepsCELQuoting guards the reason comment preservation
// matters most: a definition is full of expressions whose quoting a careless
// re-render could legitimately change.
func TestInjectDevEnvResourceKeepsCELQuoting(t *testing.T) {
	const definition = "flows:\n  - id: f\n    steps:\n      - set: 'has(msg.body.id) ? \"y\" : \"n\"'\n"
	got := injectDevEnvResource(definition)
	if !strings.Contains(got, `has(msg.body.id) ? "y" : "n"`) {
		t.Fatalf("rendered definition mangled the CEL expression:\n%s", got)
	}
}

// TestInjectDevEnvResourceLeavesBrokenInputAlone: the runtime validates the document
// and reports it far better than this could.
func TestInjectDevEnvResourceLeavesBrokenInputAlone(t *testing.T) {
	for name, definition := range map[string]string{
		"unparseable":      "service: [unclosed\n",
		"empty":            "",
		"comment only":     "# nothing here\n",
		"not a mapping":    "- one\n- two\n",
		"resources scalar": "resources: nope\n",
		"env is a mapping": "resources:\n  env:\n    a: b\n",
		"env is a scalar":  "resources:\n  env: .env.one\n",
	} {
		if got := injectDevEnvResource(definition); got != definition {
			t.Errorf("%s: definition was rewritten:\nin:  %q\nout: %q", name, definition, got)
		}
	}
}

// TestGenerationTracksContent is the property a timestamp marker could not give: the
// marker changes if and only if the content does, including when a resource is
// REMOVED — the case where "latest last_updated" would move backwards and repeat a
// value the sidecar had already applied.
func TestGenerationTracksContent(t *testing.T) {
	base := buildBundle("service:\n  name: orders\n", []Resource{
		{Name: ".env.shared", Kind: "env", Content: "A=1"},
		{Name: "body.tmpl", Kind: "template", Content: "hi"},
	})
	same := buildBundle("service:\n  name: orders\n", []Resource{
		{Name: ".env.shared", Kind: "env", Content: "A=1"},
		{Name: "body.tmpl", Kind: "template", Content: "hi"},
	})
	if base.Generation != same.Generation {
		t.Fatalf("identical content produced %q then %q", base.Generation, same.Generation)
	}

	for name, changed := range map[string]Bundle{
		"definition edited": buildBundle("service:\n  name: invoices\n", base.Resources),
		"content edited": buildBundle("service:\n  name: orders\n", []Resource{
			{Name: ".env.shared", Kind: "env", Content: "A=2"},
			{Name: "body.tmpl", Kind: "template", Content: "hi"},
		}),
		"resource removed": buildBundle("service:\n  name: orders\n", []Resource{
			{Name: ".env.shared", Kind: "env", Content: "A=1"},
		}),
		"resource renamed": buildBundle("service:\n  name: orders\n", []Resource{
			{Name: ".env.other", Kind: "env", Content: "A=1"},
			{Name: "body.tmpl", Kind: "template", Content: "hi"},
		}),
	} {
		if changed.Generation == base.Generation {
			t.Errorf("%s: generation did not change (%q)", name, base.Generation)
		}
	}
}

// TestGenerationResistsFieldRunTogether is what the length prefixes in the digest are
// for: two different splits of the same concatenated bytes must not collide.
func TestGenerationResistsFieldRunTogether(t *testing.T) {
	a := buildBundle("x", []Resource{{Name: "ab", Kind: "env", Content: "c"}})
	b := buildBundle("x", []Resource{{Name: "a", Kind: "env", Content: "bc"}})
	if a.Generation == b.Generation {
		t.Fatalf("two different resource splits share generation %q", a.Generation)
	}
}

func TestBuildBundleCarriesResourcesThrough(t *testing.T) {
	files := []Resource{{Name: ".env.dev", Kind: "env", Content: "TOKEN=x"}}
	got := buildBundle("service:\n  name: orders\n", files)
	if len(got.Resources) != 1 || got.Resources[0].Content != "TOKEN=x" {
		t.Fatalf("resources = %+v, want them passed through untouched", got.Resources)
	}
	if env := envList(t, got.Definition); !equalStrings(env, []string{devEnvResource}) {
		t.Fatalf("resources.env = %v, want %q declared", env, devEnvResource)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
