package schema

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/juancavallotti/octo/core"
)

// Generate builds the capability catalogue from a schema metadata registry,
// reflecting each block's/connector's settings struct into its fields. Blocks
// and connectors are emitted in registration order. A settings struct with a bad
// tag or an uninferable field type fails generation with a located error.
func Generate(reg *core.SchemaRegistry) (Capabilities, error) {
	caps := Capabilities{Blocks: []Block{}, Connectors: []Connector{}}

	for _, b := range reg.Blocks() {
		block := Block{
			Type:        b.Type,
			Label:       b.Label,
			Category:    b.Category,
			Group:       b.Group,
			Icon:        b.Icon,
			Description: b.Description,
			Fields:      []Field{},
		}
		if b.Config != nil {
			fields, err := fieldsOf(b.Config)
			if err != nil {
				return Capabilities{}, fmt.Errorf("block %q: %w", b.Type, err)
			}
			applyDescriptions(fields, b.SrcDir, b.Config)
			block.Fields = fields
		}
		caps.Blocks = append(caps.Blocks, block)
	}

	for _, c := range reg.Connectors() {
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
				return Capabilities{}, fmt.Errorf("connector %q settings: %w", c.Type, err)
			}
			applyDescriptions(fields, c.SrcDir, c.Settings)
			conn.Settings = fields
		}
		for _, s := range c.Sources {
			src := Source{Type: s.Type, Label: s.Label, Icon: s.Icon, Fields: []Field{}}
			if s.Settings != nil {
				fields, err := fieldsOf(s.Settings)
				if err != nil {
					return Capabilities{}, fmt.Errorf("connector %q source %q: %w", c.Type, s.Type, err)
				}
				applyDescriptions(fields, s.SrcDir, s.Settings)
				src.Fields = fields
			}
			conn.Sources = append(conn.Sources, src)
		}
		caps.Connectors = append(caps.Connectors, conn)
	}

	return caps, nil
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
		return nil, err
	}
	return buf.Bytes(), nil
}
