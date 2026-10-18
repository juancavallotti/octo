package runtime

import (
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/schema"
	"github.com/juancavallotti/octo/runtime/types"
)

// Issue is one problem Validate found in a config. Where is the block address
// (`orders.checkHeader[else].api-call`), the flow, or the connector the problem
// sits on; it is empty for a problem with the config as a whole.
type Issue struct {
	Where   string `json:"where,omitempty"`
	Message string `json:"message"`
}

// Validate checks a loaded config without starting anything: every block and
// connector type is registered, every processor ref names a processor, every
// source binds to exactly one connector, and every setting the schema marks as a
// reference (ref=connector, ref=flow, ref=template) names something the config
// declares, of the kind the setting wants.
//
// It is static on purpose. Building a flow needs its connectors started — a sql
// block takes the open database handle at build time — and starting them needs the
// credentials and the network an editor validating a draft does not have. What it
// cannot see is what only a build sees: a malformed CEL expression, or a setting
// a block's own factory refuses.
func Validate(
	cfg types.Config, connectors *core.Registry, blocks *core.BlockRegistry, meta *core.SchemaRegistry,
) ([]Issue, error) {
	caps, err := schema.Generate(meta)
	if err != nil {
		return nil, fmt.Errorf("generate schema: %w", err)
	}
	v := newValidator(cfg, connectors, blocks, meta, caps)
	v.checkConnectors()
	v.checkProcessors()
	for i := range cfg.Flows {
		v.checkFlow(&cfg.Flows[i])
	}
	return v.issues, nil
}

// validator holds the indexes one Validate pass reads its references against.
type validator struct {
	cfg        types.Config
	connectors *core.Registry
	blocks     *core.BlockRegistry
	meta       *core.SchemaRegistry

	blockSpecs     map[string]schema.Block
	connectorSpecs map[string]schema.Connector
	// declared indexes the config's connectors by name, to their type.
	declared   map[string]string
	processors map[string]string
	flows      map[string]struct{}
	templates  map[string]struct{}

	issues []Issue
}

func newValidator(
	cfg types.Config, connectors *core.Registry, blocks *core.BlockRegistry,
	meta *core.SchemaRegistry, caps schema.Capabilities,
) *validator {
	v := &validator{
		cfg:            cfg,
		connectors:     connectors,
		blocks:         blocks,
		meta:           meta,
		blockSpecs:     make(map[string]schema.Block, len(caps.Blocks)),
		connectorSpecs: make(map[string]schema.Connector, len(caps.Connectors)),
		declared:       make(map[string]string, len(cfg.Connectors)),
		processors:     make(map[string]string, len(cfg.Processors)),
		flows:          make(map[string]struct{}, len(cfg.Flows)),
		templates:      make(map[string]struct{}, len(cfg.Resources.Templates)),
	}
	for _, b := range caps.Blocks {
		v.blockSpecs[b.Type] = b
	}
	for _, c := range caps.Connectors {
		v.connectorSpecs[c.Type] = c
	}
	for _, c := range cfg.Connectors {
		v.declared[c.Name] = c.Type
	}
	for _, p := range cfg.Processors {
		v.processors[p.Name] = p.Type
	}
	// Only top-level flows are registered by name, so only they can be a flow-ref
	// target; a sub-flow's name labels it and nothing else.
	for _, f := range cfg.Flows {
		if f.Name != "" {
			v.flows[f.Name] = struct{}{}
		}
	}
	for _, t := range cfg.Resources.Templates {
		v.templates[t.Resource] = struct{}{}
		if t.As != "" {
			v.templates[t.As] = struct{}{}
		}
	}
	return v
}

func (v *validator) raise(where, format string, args ...any) {
	v.issues = append(v.issues, Issue{Where: where, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) checkConnectors() {
	for _, c := range v.cfg.Connectors {
		where := "connector " + c.Name
		if !v.connectors.Has(c.Type) {
			v.raise(where, "unknown connector type %q", c.Type)
			continue
		}
		v.checkFields(where, v.connectorSpecs[c.Type].Settings, block(c.Settings))
	}
}

func (v *validator) checkProcessors() {
	for _, p := range v.cfg.Processors {
		where := "processor " + p.Name
		if _, ok := v.blocks.Factory(p.Type); !ok {
			v.raise(where, "unknown block type %q", p.Type)
			continue
		}
		v.checkFields(where, v.blockSpecs[p.Type].Fields, block(p.Settings))
	}
}

func (v *validator) checkFlow(flow *types.FlowConfig) {
	label := flow.Name
	if label == "" {
		label = "(unnamed flow)"
	}
	if flow.Source != nil {
		v.checkSource(label, *flow.Source)
	}
	v.checkChain(label, flow.Process)
	if len(flow.Error) > 0 {
		v.checkChain(label+"[error]", flow.Error)
	}
}

// checkSource answers what resolveConnector will: the source binds to a declared
// instance, or to the one declared connector of its type, or to a default
// instance of a registered type — and two declared connectors of that type with
// no explicit binding is ambiguous.
func (v *validator) checkSource(where string, src types.SourceConfig) {
	where += " source"
	if _, ok := v.declared[src.Connector]; src.Connector != "" && ok {
		v.checkSourceFields(where, v.declared[src.Connector], src)
		return
	}
	typeName := ""
	switch {
	case src.Connector != "" && v.connectors.Has(src.Connector):
		typeName = src.Connector
	case src.Type != "" && v.connectors.Has(src.Type):
		typeName = src.Type
	}
	if typeName == "" {
		v.raise(where, "connector %q is not configured", src.Connector)
		return
	}
	var matches []string
	for _, c := range v.cfg.Connectors {
		if c.Type == typeName {
			matches = append(matches, c.Name)
		}
	}
	if len(matches) > 1 {
		v.raise(where, "%d connectors of type %q are configured (%s); set the source's connector explicitly",
			len(matches), typeName, strings.Join(matches, ", "))
		return
	}
	v.checkSourceFields(where, typeName, src)
}

func (v *validator) checkSourceFields(where, connectorType string, src types.SourceConfig) {
	for _, s := range v.connectorSpecs[connectorType].Sources {
		if s.Type == src.Type {
			v.checkFields(where, s.Fields, block(src.Settings))
			return
		}
	}
}

// checkChain checks each block of a chain, descending into the sub-flows a
// composite holds. The chain is walked as the generic tree the address resolver
// walks, for the same reason: a block's sub-flows live in its own settings, and
// only its schema says which keys hold them.
func (v *validator) checkChain(where string, chain []types.BlockConfig) {
	generic, err := toGeneric(chain)
	if err != nil {
		v.raise(where, "%v", err)
		return
	}
	v.checkGenericChain(where, generic)
}

func (v *validator) checkGenericChain(where string, chain []any) {
	for i, entry := range chain {
		b := asBlock(entry)
		label := core.BlockLabel(b.str("name"), b.str("type"), b.str("ref"))
		if label == "" {
			label = fmt.Sprintf("#%d", i)
		}
		v.checkBlock(where+"."+label, b)
	}
}

func (v *validator) checkBlock(where string, b block) {
	blockType := b.str("type")
	if ref := b.str("ref"); blockType == "" && ref != "" {
		resolved, ok := v.processors[ref]
		if !ok {
			v.raise(where, "ref %q is not a defined processor", ref)
			return
		}
		blockType = resolved
	}
	if _, ok := v.blocks.Factory(blockType); !ok {
		v.raise(where, "unknown block type %q", blockType)
		return
	}
	spec := v.blockSpecs[blockType]
	v.checkFields(where, spec.Fields, b)

	branches, err := schema.Branches(v.meta, blockType)
	if err != nil || branches == nil {
		return
	}
	for _, name := range branches.Named {
		if chain, ok := namedChain(b, name); ok {
			v.checkGenericChain(fmt.Sprintf("%s[%s]", where, name), chain)
		}
	}
	for _, slot := range branches.ByMember {
		for i, member := range members(b, slot) {
			chain, ok := member[core.BranchProcess].([]any)
			if !ok {
				continue
			}
			v.checkGenericChain(fmt.Sprintf("%s[%s]", where, core.MemberBranch(member.str("name"), i)), chain)
		}
	}
}

// checkFields checks every reference-typed setting, and descends into object
// fields whose own fields may hold references.
func (v *validator) checkFields(where string, fields []schema.Field, settings block) {
	for _, f := range fields {
		value, ok := settings.slot(f.Name)
		if !ok {
			continue
		}
		if f.Ref != nil {
			if name, isString := value.(string); isString && name != "" {
				v.checkRef(where, f, name)
			}
			continue
		}
		if len(f.Fields) > 0 {
			if nested, isMap := value.(map[string]any); isMap {
				v.checkFields(where, f.Fields, nested)
			}
		}
	}
}

func (v *validator) checkRef(where string, f schema.Field, name string) {
	switch f.Ref.Kind {
	case "flow":
		if _, ok := v.flows[name]; !ok {
			v.raise(where, "%s: flow %q is not defined", f.Name, name)
		}
	case "template":
		if _, ok := v.templates[name]; !ok {
			v.raise(where, "%s: template %q is not declared under resources.templates", f.Name, name)
		}
	case "connector":
		v.checkConnectorRef(where, f, name)
	}
}

func (v *validator) checkConnectorRef(where string, f schema.Field, name string) {
	connectorType, ok := v.declared[name]
	if !ok {
		v.raise(where, "%s: connector %q is not configured", f.Name, name)
		return
	}
	switch want := f.Ref; {
	case want.ConnectorType != "" && connectorType != want.ConnectorType:
		v.raise(where, "%s: connector %q is a %s connector, want %s", f.Name, name, connectorType, want.ConnectorType)
	case want.ConnectorCategory != "" && v.connectorSpecs[connectorType].Category != want.ConnectorCategory:
		v.raise(where, "%s: connector %q is a %s connector, want a %s connector",
			f.Name, name, connectorType, want.ConnectorCategory)
	}
}
