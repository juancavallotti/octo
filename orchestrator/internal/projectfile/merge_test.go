package projectfile

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A single file is the shape every integration is in today, so the thing worth
// pinning is that we hand it back untouched — comments, block scalars and all.
func TestMergeSingleFileIsVerbatim(t *testing.T) {
	content := `service:
  name: orders

# this comment must survive
flows:
  - name: intake
    source:
      connector: http
    steps:
      - type: log
        message: |
          a block scalar
          across lines
`
	got, err := Merge([]File{{Path: "orders.yaml", Content: content}})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got != content {
		t.Errorf("single file was reformatted:\n%s", got)
	}
}

func TestMergeConcatenates(t *testing.T) {
	a := File{Path: "a.yaml", Content: `service:
  name: orders
connectors:
  - name: api
    type: http
flows:
  - name: intake
`}
	b := File{Path: "b.yaml", Content: `connectors:
  - name: db
    type: postgres
processors:
  - name: clean
flows:
  - name: dispatch
`}

	got, err := Merge([]File{b, a}) // out of order on purpose
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var cfg struct {
		Service    map[string]string   `yaml:"service"`
		Connectors []map[string]string `yaml:"connectors"`
		Processors []map[string]string `yaml:"processors"`
		Flows      []map[string]string `yaml:"flows"`
	}
	if err := yaml.Unmarshal([]byte(got), &cfg); err != nil {
		t.Fatalf("merged output does not parse: %v\n%s", err, got)
	}

	if cfg.Service["name"] != "orders" {
		t.Errorf("service = %v, want orders", cfg.Service)
	}
	if len(cfg.Connectors) != 2 || cfg.Connectors[0]["name"] != "api" || cfg.Connectors[1]["name"] != "db" {
		t.Errorf("connectors = %v, want api then db (lexical file order)", cfg.Connectors)
	}
	if len(cfg.Processors) != 1 {
		t.Errorf("processors = %v, want 1", cfg.Processors)
	}
	if len(cfg.Flows) != 2 || cfg.Flows[0]["name"] != "intake" || cfg.Flows[1]["name"] != "dispatch" {
		t.Errorf("flows = %v, want intake then dispatch", cfg.Flows)
	}
}

// The runtime refuses these merges at load. Finding out here, before we store,
// is the reason the merge validates at all.
func TestMergeRejectsConflicts(t *testing.T) {
	cases := []struct {
		name  string
		files []File
		want  string
	}{
		{
			name: "duplicate flow",
			files: []File{
				{Path: "a.yaml", Content: "flows:\n  - name: intake\n"},
				{Path: "b.yaml", Content: "flows:\n  - name: intake\n"},
			},
			want: `flow "intake" is defined in both a.yaml and b.yaml`,
		},
		{
			name: "duplicate connector",
			files: []File{
				{Path: "a.yaml", Content: "connectors:\n  - name: api\n"},
				{Path: "b.yaml", Content: "connectors:\n  - name: api\n"},
			},
			want: `connector "api" is defined in both a.yaml and b.yaml`,
		},
		{
			name: "duplicate processor",
			files: []File{
				{Path: "a.yaml", Content: "processors:\n  - name: clean\n"},
				{Path: "b.yaml", Content: "processors:\n  - name: clean\n"},
			},
			want: `processor "clean" is defined in both a.yaml and b.yaml`,
		},
		{
			name: "two service identities",
			files: []File{
				{Path: "a.yaml", Content: "service:\n  name: one\n"},
				{Path: "b.yaml", Content: "service:\n  name: two\n"},
			},
			want: "service identity is declared in a.yaml and b.yaml",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Merge(c.files)
			if !errors.Is(err, ErrMerge) {
				t.Fatalf("err = %v, want ErrMerge", err)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %q, want it to name both files: %q", err, c.want)
			}
		})
	}
}

// Declarations are allowed to repeat across files — the same variable used in
// two flows is normal. First one wins, as it does in the runtime.
func TestMergeDedupesDeclarations(t *testing.T) {
	files := []File{
		{Path: "a.yaml", Content: `env:
  - name: TOKEN
    required: true
resources:
  env:
    - .env.dev
  templates:
    - resource: templates/welcome.tmpl
      as: welcome
`},
		{Path: "b.yaml", Content: `env:
  - name: TOKEN
    required: false
resources:
  env:
    - .env.dev
    - .env.extra
  templates:
    - resource: templates/other.tmpl
      as: welcome
`},
	}

	got, err := Merge(files)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	var cfg struct {
		Env       []map[string]any `yaml:"env"`
		Resources struct {
			Env       []string            `yaml:"env"`
			Templates []map[string]string `yaml:"templates"`
		} `yaml:"resources"`
	}
	if err := yaml.Unmarshal([]byte(got), &cfg); err != nil {
		t.Fatalf("merged output does not parse: %v\n%s", err, got)
	}

	if len(cfg.Env) != 1 || cfg.Env[0]["required"] != true {
		t.Errorf("env = %v, want one TOKEN with the first declaration winning", cfg.Env)
	}
	if len(cfg.Resources.Env) != 2 {
		t.Errorf("resources.env = %v, want .env.dev deduped", cfg.Resources.Env)
	}
	if len(cfg.Resources.Templates) != 1 || cfg.Resources.Templates[0]["resource"] != "templates/welcome.tmpl" {
		t.Errorf("templates = %v, want dedup by alias keeping the first", cfg.Resources.Templates)
	}
}

// A key we do not merge is still a key somebody wrote. Carrying it through
// beats dropping it because the decoder happens to ignore it.
func TestMergeKeepsUnknownKeys(t *testing.T) {
	files := []File{
		{Path: "a.yaml", Content: "flows:\n  - name: intake\nsomethingNew: kept\n"},
		{Path: "b.yaml", Content: "flows:\n  - name: dispatch\n"},
	}
	got, err := Merge(files)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !strings.Contains(got, "somethingNew: kept") {
		t.Errorf("unknown key was dropped:\n%s", got)
	}
}

func TestMergeEmptyInputs(t *testing.T) {
	got, err := Merge(nil)
	if err != nil || got != "" {
		t.Fatalf("Merge(nil) = %q, %v; want empty", got, err)
	}

	// an empty file among real ones contributes nothing rather than failing
	got, err = Merge([]File{
		{Path: "a.yaml", Content: "flows:\n  - name: intake\n"},
		{Path: "b.yaml", Content: "\n"},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !strings.Contains(got, "intake") {
		t.Errorf("lost the real file:\n%s", got)
	}
}
