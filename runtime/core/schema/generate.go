package schema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
)

// Generate builds the capability catalogue from a schema metadata registry,
// reflecting each block's/connector's settings struct into its fields. Blocks
// and connectors are emitted in registration order. A settings struct with a bad
// tag or an uninferable field type fails generation with a located error.
func Generate(reg *core.SchemaRegistry) (Capabilities, error) {
	caps := Capabilities{Blocks: []Block{}, Connectors: []Connector{}}

	for _, b := range reg.Blocks() {
		block, err := blockSpec(b)
		if err != nil {
			return Capabilities{}, err
		}
		caps.Blocks = append(caps.Blocks, block)
	}

	for _, c := range reg.Connectors() {
		conn, err := connectorSpec(c)
		if err != nil {
			return Capabilities{}, err
		}
		caps.Connectors = append(caps.Connectors, conn)
	}

	return caps, nil
}

// blockSpec builds one Block from its registered metadata, reflecting its
// settings struct (if any) into fields and overlaying doc-comment descriptions.
func blockSpec(b core.BlockMeta) (Block, error) {
	block := Block{
		Type:        b.Type,
		Label:       b.Label,
		Category:    b.Category,
		Group:       b.Group,
		Icon:        b.Icon,
		Description: b.Description,
		Fields:      []Field{},
	}
	if b.Config == nil {
		return block, nil
	}
	fields, err := fieldsOf(b.Config)
	if err != nil {
		return Block{}, fmt.Errorf("block %q: %w", b.Type, err)
	}
	applyDescriptions(fields, b.SrcDir, b.Config)
	block.Fields = fields
	block.AddressBranches = addressBranchesOf(b.Type, fields)
	return block, nil
}

// connectorSpec builds one Connector from its metadata, reflecting its settings
// struct and each of its sources.
func connectorSpec(c core.ConnectorMeta) (Connector, error) {
	conn := Connector{
		Type:     c.Type,
		Label:    c.Label,
		Icon:     c.Icon,
		Category: c.Category,
		Settings: []Field{},
		Sources:  []Source{},
	}
	if c.Settings != nil {
		fields, err := fieldsOf(c.Settings)
		if err != nil {
			return Connector{}, fmt.Errorf("connector %q settings: %w", c.Type, err)
		}
		applyDescriptions(fields, c.SrcDir, c.Settings)
		conn.Settings = fields
	}
	for _, s := range c.Sources {
		src, err := sourceSpec(c.Type, s)
		if err != nil {
			return Connector{}, err
		}
		conn.Sources = append(conn.Sources, src)
	}
	for _, t := range c.ProviderTools {
		tool, err := providerToolSpec(c.Type, c.SrcDir, t)
		if err != nil {
			return Connector{}, err
		}
		conn.ProviderTools = append(conn.ProviderTools, tool)
	}
	return conn, nil
}

// providerToolSpec builds one ProviderTool from its metadata. connType names the
// owning connector for error context; srcDir is where its settings struct's doc
// comments are read from.
func providerToolSpec(connType, srcDir string, t core.ProviderToolMeta) (ProviderTool, error) {
	tool := ProviderTool{Type: t.Type, Label: t.Label, Description: t.Description, Fields: []Field{}}
	if t.Settings == nil {
		return tool, nil
	}
	fields, err := fieldsOf(t.Settings)
	if err != nil {
		return ProviderTool{}, fmt.Errorf("connector %q provider tool %q: %w", connType, t.Type, err)
	}
	applyDescriptions(fields, srcDir, t.Settings)
	tool.Fields = fields
	return tool, nil
}

// sourceSpec builds one Source from its metadata. connType names the owning
// connector for error context.
func sourceSpec(connType string, s core.SourceMeta) (Source, error) {
	src := Source{Type: s.Type, Label: s.Label, Icon: s.Icon, Fields: []Field{}}
	if s.Settings == nil {
		return src, nil
	}
	fields, err := fieldsOf(s.Settings)
	if err != nil {
		return Source{}, fmt.Errorf("connector %q source %q: %w", connType, s.Type, err)
	}
	applyDescriptions(fields, s.SrcDir, s.Settings)
	src.Fields = fields
	return src, nil
}

// Marshal renders a catalogue as the canonical capabilities.json bytes: 2-space
// indented, HTML escaping off (so &, <, > stay literal), with a trailing
// newline (json.Encoder appends one).
func Marshal(caps Capabilities) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(caps); err != nil {
		return nil, fmt.Errorf("encoding capabilities: %w", err)
	}
	return buf.Bytes(), nil
}
