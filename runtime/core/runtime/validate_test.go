package runtime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// vconn is a connector that is never started: Validate only asks whether its type
// is registered.
type vconn struct{}

func (vconn) Start(context.Context, types.ConnectorConfig) error { return nil }
func (vconn) Stop(context.Context) error                         { return nil }

type vcallSettings struct {
	DB       string `json:"db" octo:"label=Database,ref=connector-category:database"`
	Client   string `json:"client" octo:"label=Client,ref=connector:vhttp"`
	Template string `json:"template" octo:"label=Template,ref=template"`
}

type vrefSettings struct {
	Flow string `json:"flow" octo:"label=Flow,ref=flow"`
}

type vforkSettings struct {
	Branches []struct {
		Name    string              `json:"name"`
		Process []types.BlockConfig `json:"process"`
	} `json:"branches" octo:"label=Branches,type=flow-list"`
}

// validationRegistries registers a small, self-contained set of blocks and
// connectors, so the test pins Validate's walk rather than any real block's schema.
func validationRegistries() (*core.Registry, *core.BlockRegistry, *core.SchemaRegistry) {
	connectors := core.NewRegistry()
	blocks := core.NewBlockRegistry()
	meta := core.NewSchemaRegistry()

	newConn := func() core.Connector { return vconn{} }
	for _, c := range []struct{ typ, category string }{
		{"vsql", "database"}, {"vhttp", "http"}, {"vcron", "schedule"},
	} {
		connectors.MustRegister(c.typ, newConn)
		meta.AddConnector(core.ConnectorMeta{Type: c.typ, Label: c.typ, Category: c.category})
	}

	noop := func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) { return nil, nil }
	for typ, cfg := range map[string]any{
		"vcall": vcallSettings{}, "vref": vrefSettings{}, "vfork": vforkSettings{},
	} {
		blocks.MustRegister(typ, noop)
		meta.AddBlock(core.BlockMeta{Type: typ, Label: typ, Config: reflect.TypeOf(cfg)})
	}
	return connectors, blocks, meta
}

const validProject = `
connectors:
  - name: orders-db
    type: vsql
  - name: api
    type: vhttp
resources:
  templates:
    - resource: templates/receipt.tmpl
      as: receipt
processors:
  - name: save
    type: vcall
    settings:
      db: orders-db
flows:
  - name: nightly
    source:
      connector: vcron
      type: tick
    process:
      - ref: save
      - type: vfork
        name: fan
        settings:
          branches:
            - name: notify
              process:
                - type: vcall
                  settings:
                    client: api
                    template: receipt
                - type: vref
                  settings:
                    flow: audit
  - name: audit
    process:
      - type: vcall
        settings:
          db: orders-db
`

// onDisk is a resource loader holding a fixed set of template files.
type onDisk map[string]string

func (d onDisk) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	if content, ok := d[id]; ok {
		return []byte(content), nil
	}
	return nil, errors.New("not found")
}

// projectFiles is what the resource root of validProject holds.
var projectFiles = onDisk{"templates/receipt.tmpl": "receipt", "templates/invoice.tmpl": "invoice"}

func TestValidateAcceptsAConsistentConfig(t *testing.T) {
	cfg, err := ParseConfig([]byte(validProject))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	connectors, blocks, meta := validationRegistries()
	issues, err := Validate(cfg, connectors, blocks, meta, projectFiles)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("issues = %+v, want none", issues)
	}
}

// TestValidateAcceptsATemplateByResourceID: a run resolves a template a block names
// by its resource id whether or not it is declared, so validation must too — by its
// alias, by its declared id, or by an id the resource root holds.
func TestValidateAcceptsATemplateByResourceID(t *testing.T) {
	connectors, blocks, meta := validationRegistries()
	for _, name := range []string{"receipt", "templates/receipt.tmpl", "templates/invoice.tmpl"} {
		cfg, err := ParseConfig([]byte(strings.Replace(validProject, "template: receipt", "template: "+name, 1)))
		if err != nil {
			t.Fatalf("ParseConfig: %v", err)
		}
		issues, err := Validate(cfg, connectors, blocks, meta, projectFiles)
		if err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if len(issues) != 0 {
			t.Errorf("template %q: issues = %+v, want none", name, issues)
		}
	}
}

func TestValidateReportsBrokenReferences(t *testing.T) {
	tests := []struct {
		name      string
		edit      func(string) string
		wantWhere string
		wantMsg   string
	}{
		{
			name:      "flow-ref to a flow nobody declares, inside a branch",
			edit:      func(s string) string { return strings.Replace(s, "flow: audit", "flow: audits", 1) },
			wantWhere: "nightly.fan[notify].vref",
			wantMsg:   `flow: flow "audits" is not defined`,
		},
		{
			name:      "connector of the wrong category",
			edit:      func(s string) string { return replaceLast(s, "db: orders-db", "db: api") },
			wantWhere: "audit.vcall",
			wantMsg:   `db: connector "api" is a vhttp connector, want a database connector`,
		},
		{
			name:      "connector of the wrong type",
			edit:      func(s string) string { return strings.Replace(s, "client: api", "client: orders-db", 1) },
			wantWhere: "nightly.fan[notify].vcall",
			wantMsg:   `client: connector "orders-db" is a vsql connector, want vhttp`,
		},
		{
			name:      "connector nobody declares, in a processor",
			edit:      func(s string) string { return strings.Replace(s, "db: orders-db", "db: ledger", 1) },
			wantWhere: "processor save",
			wantMsg:   `db: connector "ledger" is not configured`,
		},
		{
			name: "undeclared template that does not load",
			edit: func(s string) string {
				return strings.Replace(s, "template: receipt", "template: templates/missing.tmpl", 1)
			},
			wantWhere: "nightly.fan[notify].vcall",
			wantMsg:   `template: template "templates/missing.tmpl" is not declared under resources.templates, and does not load`,
		},
		{
			name:      "ref to a missing processor",
			edit:      func(s string) string { return strings.Replace(s, "- ref: save", "- ref: persist", 1) },
			wantWhere: "nightly.persist",
			wantMsg:   `ref "persist" is not a defined processor`,
		},
		{
			name:      "unknown block type",
			edit:      func(s string) string { return strings.Replace(s, "- type: vref", "- type: vreff", 1) },
			wantWhere: "nightly.fan[notify].vreff",
			wantMsg:   `unknown block type "vreff"`,
		},
		{
			name:      "source on a connector nobody declares",
			edit:      func(s string) string { return strings.Replace(s, "connector: vcron", "connector: clock", 1) },
			wantWhere: "nightly source",
			wantMsg:   `connector "clock" is not configured`,
		},
		{
			name: "source with two candidates",
			edit: func(s string) string {
				s = strings.Replace(s, "connector: vcron", "connector: vsql", 1)
				return strings.Replace(s, "connectors:\n", "connectors:\n  - name: replica\n    type: vsql\n", 1)
			},
			wantWhere: "nightly source",
			wantMsg:   `2 connectors of type "vsql" are configured (replica, orders-db)`,
		},
	}
	connectors, blocks, meta := validationRegistries()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tc.edit(validProject)))
			if err != nil {
				t.Fatalf("ParseConfig: %v", err)
			}
			issues, err := Validate(cfg, connectors, blocks, meta, projectFiles)
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if len(issues) != 1 {
				t.Fatalf("issues = %+v, want exactly one", issues)
			}
			if issues[0].Where != tc.wantWhere || !strings.Contains(issues[0].Message, tc.wantMsg) {
				t.Fatalf("issue = %+v, want where %q and message containing %q", issues[0], tc.wantWhere, tc.wantMsg)
			}
		})
	}
}

// replaceLast replaces the last occurrence of old in s.
func replaceLast(s, old, replacement string) string {
	i := strings.LastIndex(s, old)
	return s[:i] + replacement + s[i+len(old):]
}
