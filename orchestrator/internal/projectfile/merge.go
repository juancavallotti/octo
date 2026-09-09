package projectfile

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlIndent matches the indentation every config in this repository is written
// with; yaml.v3 would otherwise emit four spaces.
const yamlIndent = 2

// Top-level keys the merge understands. Anything else is carried through
// first-wins rather than dropped — the runtime's decoder ignores keys it does
// not know, and silently losing one here would be worse than keeping it.
const (
	keyService    = "service"
	keyEnv        = "env"
	keyResources  = "resources"
	keyConnectors = "connectors"
	keyProcessors = "processors"
	keyFlows      = "flows"
)

// keyOrder is the order the merged document is emitted in, matching how the
// configs in samples/ are written.
var keyOrder = []string{keyService, keyEnv, keyResources, keyConnectors, keyProcessors, keyFlows}

// ErrMerge is the class of failure where several config files cannot be one
// config: a name claimed twice, or a service identity declared more than once.
var ErrMerge = errors.New("config files conflict")

// Merge folds an integration's config files into the single definition the rest
// of the orchestrator still speaks in.
//
// The rules are runtime.MergeConfigs': connectors, processors and flows
// concatenate with duplicate names rejected, env declarations concatenate with
// the first winning, declared resources de-duplicate, and the service identity
// may come from exactly one file.
//
// One file is returned verbatim. That is the case every integration is in
// today, and it matters that it stays byte-for-byte — a round trip through the
// YAML encoder would reflow comments and block scalars that a user wrote by
// hand, and nobody asked us to reformat their config to read it back.
//
// The claim worth re-checking when either side moves: loading this output with
// runtime.LoadConfig must produce the same types.Config as pointing LoadConfig
// at the folder. That was verified against the real loader before this landed,
// and it is the only statement here the unit tests below cannot make on their
// own — they live on the wrong side of the module boundary to import it.
func Merge(files []File) (string, error) {
	sorted := make([]File, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	switch len(sorted) {
	case 0:
		return "", nil
	case 1:
		return sorted[0].Content, nil
	}

	m := newMerger()
	for _, f := range sorted {
		if err := m.add(f); err != nil {
			return "", err
		}
	}
	return m.encode()
}

// File is one file of a project: its path within the folder and its content.
type File struct {
	Path    string
	Content string
}

// merger accumulates the top-level keys across documents while enforcing the
// uniqueness the runtime enforces at load.
type merger struct {
	service     *yaml.Node
	serviceFile string

	env       []*yaml.Node
	envSeen   map[string]struct{}
	resEnv    []*yaml.Node
	resSeen   map[string]struct{}
	templates []*yaml.Node
	tplSeen   map[string]struct{}

	connectors []*yaml.Node
	processors []*yaml.Node
	flows      []*yaml.Node
	named      map[string]map[string]string // kind -> name -> file that claimed it

	extra     map[string]*yaml.Node
	extraKeys []string
}

func newMerger() *merger {
	return &merger{
		envSeen: map[string]struct{}{},
		resSeen: map[string]struct{}{},
		tplSeen: map[string]struct{}{},
		named: map[string]map[string]string{
			keyConnectors: {},
			keyProcessors: {},
			keyFlows:      {},
		},
		extra: map[string]*yaml.Node{},
	}
}

// add folds one document into the merge.
func (m *merger) add(f File) error {
	root, err := documentRoot(f)
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i].Value, root.Content[i+1]
		if err := m.addKey(f, key, value); err != nil {
			return err
		}
	}
	return nil
}

func (m *merger) addKey(f File, key string, value *yaml.Node) error {
	switch key {
	case keyService:
		if m.service != nil {
			return fmt.Errorf("%w: service identity is declared in %s and %s", ErrMerge, m.serviceFile, f.Path)
		}
		m.service, m.serviceFile = value, f.Path
		return nil
	case keyEnv:
		m.env = appendDeduped(m.env, m.envSeen, value, "name")
		return nil
	case keyResources:
		m.addResources(value)
		return nil
	case keyConnectors, keyProcessors, keyFlows:
		return m.addNamed(f, key, value)
	default:
		if _, dup := m.extra[key]; !dup {
			m.extra[key] = value
			m.extraKeys = append(m.extraKeys, key)
		}
		return nil
	}
}

// addResources folds the declared env files and templates, de-duplicating the
// way the runtime does: env files by their id, templates by their alias when
// they have one and by the resource id when they do not.
func (m *merger) addResources(value *yaml.Node) {
	if value.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		switch value.Content[i].Value {
		case keyEnv:
			m.resEnv = appendDeduped(m.resEnv, m.resSeen, value.Content[i+1], "")
		case "templates":
			m.templates = appendDeduped(m.templates, m.tplSeen, value.Content[i+1], "as", "resource")
		}
	}
}

// addNamed concatenates a sequence whose entries carry a name, refusing a name
// two files both claim — the runtime would refuse the same merge at load, and
// finding out here is the whole point of validating before we store.
func (m *merger) addNamed(f File, kind string, value *yaml.Node) error {
	if value.Kind != yaml.SequenceNode {
		return nil
	}
	seen := m.named[kind]
	for _, entry := range value.Content {
		name := fieldValue(entry, "name")
		if name != "" {
			if owner, dup := seen[name]; dup {
				return fmt.Errorf("%w: %s %q is defined in both %s and %s",
					ErrMerge, strings.TrimSuffix(kind, "s"), name, owner, f.Path)
			}
			seen[name] = f.Path
		}
		switch kind {
		case keyConnectors:
			m.connectors = append(m.connectors, entry)
		case keyProcessors:
			m.processors = append(m.processors, entry)
		case keyFlows:
			m.flows = append(m.flows, entry)
		}
	}
	return nil
}

// encode emits the merged document.
func (m *merger) encode() (string, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	put := func(key string, value *yaml.Node) {
		if value == nil {
			return
		}
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
	}

	for _, key := range keyOrder {
		switch key {
		case keyService:
			put(keyService, m.service)
		case keyEnv:
			put(keyEnv, sequence(m.env))
		case keyResources:
			put(keyResources, m.resourcesNode())
		case keyConnectors:
			put(keyConnectors, sequence(m.connectors))
		case keyProcessors:
			put(keyProcessors, sequence(m.processors))
		case keyFlows:
			put(keyFlows, sequence(m.flows))
		}
	}
	for _, key := range m.extraKeys {
		put(key, m.extra[key])
	}

	var out strings.Builder
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(root); err != nil {
		return "", fmt.Errorf("encode merged config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encode merged config: %w", err)
	}
	return out.String(), nil
}

func (m *merger) resourcesNode() *yaml.Node {
	env, templates := sequence(m.resEnv), sequence(m.templates)
	if env == nil && templates == nil {
		return nil
	}
	ret := &yaml.Node{Kind: yaml.MappingNode}
	if env != nil {
		ret.Content = append(ret.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: keyEnv}, env)
	}
	if templates != nil {
		ret.Content = append(ret.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "templates"}, templates)
	}
	return ret
}

// documentRoot parses a file down to its top-level mapping. An empty file
// contributes nothing rather than failing the merge.
func documentRoot(f File) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(f.Content), &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", f.Path, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: %s is not a config mapping", ErrMerge, f.Path)
	}
	return root, nil
}

// appendDeduped folds a sequence into acc, skipping entries already seen. The
// identity is the first keys field that the entry carries; with no keys the
// entry's own scalar value is the identity, which is how resources.env works.
func appendDeduped(acc []*yaml.Node, seen map[string]struct{}, value *yaml.Node, keys ...string) []*yaml.Node {
	if value.Kind != yaml.SequenceNode {
		return acc
	}
	for _, entry := range value.Content {
		id := entry.Value
		for _, k := range keys {
			if v := fieldValue(entry, k); v != "" {
				id = v
				break
			}
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		acc = append(acc, entry)
	}
	return acc
}

// fieldValue reads a scalar field off a mapping node, empty when absent.
func fieldValue(node *yaml.Node, key string) string {
	if node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1].Value
		}
	}
	return ""
}

// sequence wraps accumulated entries, nil when there are none so the key is
// left out of the merged document entirely.
func sequence(entries []*yaml.Node) *yaml.Node {
	if len(entries) == 0 {
		return nil
	}
	return &yaml.Node{Kind: yaml.SequenceNode, Content: entries}
}
