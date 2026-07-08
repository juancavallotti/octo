// Schema metadata registry. Alongside the functional block/connector factory
// registries (block_registry.go, registry.go), a block or connector may register
// *presentation* metadata used to generate the editor capability schema
// (packages/editor/src/app/schema/capabilities.json). The metadata is authored in
// Go next to the settings struct it describes, so a single source change adds a
// block to both the runtime and the editor.
//
// A block adds one RegisterBlockMeta call to its existing init(), keyed by the
// same type string as MustRegisterBlock. Field-level metadata (label, type,
// required, default, enum, ref, showIf) rides on the settings struct as `octo`
// struct tags and is read by reflection in the core/schema package; this file
// only carries the block/connector-level metadata and the pointer (Config /
// Settings reflect.Type) to the struct the schema reflects over.
//
// Package-level defaults are supplied by RegisterExtension: a connector package
// declares a default palette Group (and Icon) once, and every block/connector it
// registers inherits it unless it sets its own. Association is by the caller's
// source directory (one directory == one package), captured via runtime.Caller.
package core

import (
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
)

// Block categories. A block is either a plain processor step or a control-flow
// composite; these are the only two valid BlockMeta.Category values. They are
// exported so every registering package names them by constant rather than by a
// repeated string literal.
const (
	CategoryProcessor   = "processor"
	CategoryControlFlow = "control-flow"
)

// BlockMeta is the editor-facing description of a block. Type must match the name
// passed to MustRegisterBlock. Category is "processor" or "control-flow". Group
// and Icon fall back to the registering package's ExtensionMeta when empty.
// Config is the zero-value settings struct whose `octo`/`json` tags describe the
// block's fields; it is nil for composite (control-flow) blocks that have no
// settings struct and instead register a schema-only meta struct.
type BlockMeta struct {
	Type        string
	Label       string
	Category    string
	Group       string
	Icon        string
	Description string
	Config      reflect.Type
	// SrcDir is the source directory of the registering package, captured
	// automatically by RegisterBlockMeta for extension association and
	// doc-comment extraction. Do not set it by hand.
	SrcDir string
}

// SourceMeta is the editor-facing description of a connector source (e.g. the
// http route source). Settings is the zero-value source-settings struct whose
// tags describe its fields; nil means the source takes no fields.
type SourceMeta struct {
	Type     string
	Label    string
	Icon     string
	Settings reflect.Type
	// SrcDir is populated from the owning connector's directory when empty.
	SrcDir string
}

// ConnectorMeta is the editor-facing description of a connector. Settings is the
// zero-value connector-settings struct whose tags describe its settings fields.
// Category groups a family of connectors (e.g. "llm") so a field can reference
// any member. Icon falls back to the package ExtensionMeta when empty.
type ConnectorMeta struct {
	Type     string
	Label    string
	Icon     string
	Category string
	Settings reflect.Type
	Sources  []SourceMeta
	// SrcDir is captured automatically by RegisterConnectorMeta.
	SrcDir string
}

// ExtensionMeta declares package-level defaults inherited by every block and
// connector registered from the same package (directory). It is registered once
// per package init(); a block/connector value overrides the default it carries.
type ExtensionMeta struct {
	Group string
	Icon  string
	// SrcDir is captured automatically by RegisterExtension.
	SrcDir string
}

// SchemaRegistry holds the registered schema metadata in registration order (the
// order the editor renders them). It is safe for concurrent registration.
type SchemaRegistry struct {
	mu         sync.Mutex
	blocks     []BlockMeta
	connectors []ConnectorMeta
	extensions map[string]ExtensionMeta // keyed by SrcDir
}

// NewSchemaRegistry returns an empty registry.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{extensions: make(map[string]ExtensionMeta)}
}

// AddBlock appends block metadata verbatim (SrcDir already set by the caller).
func (r *SchemaRegistry) AddBlock(m BlockMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocks = append(r.blocks, m)
}

// AddConnector appends connector metadata verbatim, defaulting each source's
// SrcDir to the connector's when unset.
func (r *SchemaRegistry) AddConnector(m ConnectorMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range m.Sources {
		if m.Sources[i].SrcDir == "" {
			m.Sources[i].SrcDir = m.SrcDir
		}
	}
	r.connectors = append(r.connectors, m)
}

// AddExtension records a package-level default keyed by SrcDir.
func (r *SchemaRegistry) AddExtension(m ExtensionMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extensions[m.SrcDir] = m
}

// Blocks returns the registered blocks in registration order, with Group and
// Icon resolved: an explicit block value wins over its package ExtensionMeta.
func (r *SchemaRegistry) Blocks() []BlockMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]BlockMeta, len(r.blocks))
	for i, b := range r.blocks {
		if ext, ok := r.extensions[b.SrcDir]; ok {
			if b.Group == "" {
				b.Group = ext.Group
			}
			if b.Icon == "" {
				b.Icon = ext.Icon
			}
		}
		out[i] = b
	}
	return out
}

// Connectors returns the registered connectors in registration order, with Icon
// resolved from the package ExtensionMeta when a connector sets none.
func (r *SchemaRegistry) Connectors() []ConnectorMeta {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ConnectorMeta, len(r.connectors))
	for i, c := range r.connectors {
		if ext, ok := r.extensions[c.SrcDir]; ok && c.Icon == "" {
			c.Icon = ext.Icon
		}
		out[i] = c
	}
	return out
}

var defaultSchemaRegistry = NewSchemaRegistry()

// DefaultSchemaRegistry returns the process-wide schema metadata registry.
func DefaultSchemaRegistry() *SchemaRegistry {
	return defaultSchemaRegistry
}

// RegisterBlockMeta records block metadata on the default registry, capturing the
// calling package's source directory for extension association.
func RegisterBlockMeta(m BlockMeta) {
	m.SrcDir = callerDir()
	defaultSchemaRegistry.AddBlock(m)
}

// RegisterConnectorMeta records connector metadata on the default registry.
func RegisterConnectorMeta(m ConnectorMeta) {
	m.SrcDir = callerDir()
	defaultSchemaRegistry.AddConnector(m)
}

// RegisterExtension records package-level schema defaults on the default registry.
func RegisterExtension(m ExtensionMeta) {
	m.SrcDir = callerDir()
	defaultSchemaRegistry.AddExtension(m)
}

// callerDir returns the source directory of the function that called the
// exported Register* wrapper (one level above this helper). All files of a Go
// package share a directory, so the directory identifies the package.
func callerDir() string {
	// Frames: 0 = callerDir, 1 = RegisterBlockMeta/…, 2 = the init() that called it.
	_, file, _, ok := runtime.Caller(2)
	if !ok {
		return ""
	}
	return filepath.Dir(file)
}
