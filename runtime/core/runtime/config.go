package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/dsl"
	"github.com/juancavallotti/octo/runtime/types"
)

// TestFileSuffix marks a file as a test suite rather than a config: orders_test.yaml
// tests orders.yaml. It is exported alongside IsTestFile because dolphin derives a
// suite's name from its flow file's and back again, and the two must agree — see
// IsTestFile.
const TestFileSuffix = "_test"

// LoadConfig reads and parses the runtime config at path. When path is a
// directory, every *.yaml/*.yml file in it is parsed and merged into one config
// (see MergeConfigs), skipping test files (see IsTestFile); otherwise the single
// file is parsed. loader supplies the declared env resources (resources.env)
// combined into the environment; it is the runtime-services resource loader
// (rooted at the config directory in the standalone module).
func LoadConfig(path string, loader core.ResourceLoader) (types.Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return types.Config{}, fmt.Errorf("stat config path %q: %w", path, err)
	}

	var docs []*yaml.Node
	if info.IsDir() {
		docs, err = loadDir(path)
	} else {
		docs, err = loadOne(path)
	}
	if err != nil {
		return types.Config{}, err
	}

	cfg, err := resolveDocuments(docs, loader)
	if err != nil {
		return types.Config{}, err
	}
	// Load and parse declared templates now so a missing or malformed one fails at
	// deployment, not when a message first renders it.
	if err := warmTemplates(loader, cfg.Resources.Templates); err != nil {
		return types.Config{}, err
	}
	return cfg, nil
}

// ParseConfig parses the runtime config from raw config bytes, resolving declared
// environment variables and substituting ${NAME} references.
func ParseConfig(data []byte) (types.Config, error) {
	doc, err := dsl.ParseDocument(data)
	if err != nil {
		return types.Config{}, err
	}
	// The byte-parse entrypoint has no config directory, so declared env resources
	// cannot be resolved; the no-op loader reports them missing (which is skipped).
	return resolveDocuments([]*yaml.Node{doc}, core.NoopResourceLoader{})
}

// resolveDocuments turns parsed config documents into one typed, fully resolved
// config: it reads the env declarations across all of them, resolves the
// environment once, substitutes ${NAME} references throughout each document, then
// decodes and merges.
//
// The order is the point. Substitution rewrites the YAML before it is typed, so a
// reference can fill a field of any type — workers: ${FLOW_WORKERS} as readily as
// a settings value. And declarations are gathered across every document first, so
// a variable declared in one file still serves a reference in another, exactly as
// it did when the merge came first.
func resolveDocuments(docs []*yaml.Node, loader core.ResourceLoader) (types.Config, error) {
	decls, err := mergeEnvDecls(docs)
	if err != nil {
		return types.Config{}, err
	}
	resolved, declared, err := resolveDeclared(decls, loader)
	if err != nil {
		return types.Config{}, err
	}

	configs := make([]types.Config, 0, len(docs))
	for _, doc := range docs {
		if err := substituteDocument(doc, resolved, declared); err != nil {
			return types.Config{}, err
		}
		cfg, decodeErr := dsl.Decode(doc)
		if decodeErr != nil {
			return types.Config{}, decodeErr
		}
		configs = append(configs, cfg)
	}

	cfg, err := MergeConfigs(configs)
	if err != nil {
		return types.Config{}, err
	}
	// Keep the resolved values so expressions can read them as env.NAME, not just
	// through ${NAME} substitution. The merge does not carry them: ResolvedEnv is
	// not a decoded field.
	cfg.ResolvedEnv = resolved
	return cfg, nil
}

// loadOne parses a single config file into its document node.
func loadOne(path string) ([]*yaml.Node, error) {
	data, err := dsl.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, err := dsl.ParseDocument(data)
	if err != nil {
		return nil, err
	}
	return []*yaml.Node{doc}, nil
}

// loadDir parses every YAML config file in dir, skipping test files. Files are
// parsed in lexical order so duplicate-name errors are deterministic.
func loadDir(dir string) ([]*yaml.Node, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read config dir %q: %w", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) || IsTestFile(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .yaml/.yml config files in %q", dir)
	}
	sort.Strings(paths)

	docs := make([]*yaml.Node, 0, len(paths))
	for _, p := range paths {
		doc, loadErr := loadOne(p)
		if loadErr != nil {
			return nil, loadErr
		}
		docs = append(docs, doc...)
	}
	return docs, nil
}

// mergeEnvDecls reads the env declarations and env resources from every document,
// ahead of substitution. Duplicate variable declarations across files are allowed
// and the first wins, matching how the decoded merge treats them; env resources
// keep first-seen order so the combined load order stays deterministic.
func mergeEnvDecls(docs []*yaml.Node) (envDecls, error) {
	var merged envDecls
	seenVar := make(map[string]struct{})
	seenRes := make(map[string]struct{})
	for _, doc := range docs {
		if doc == nil || doc.Kind == 0 {
			continue
		}
		var decls envDecls
		if err := doc.Decode(&decls); err != nil {
			return envDecls{}, fmt.Errorf("parse config: %w", err)
		}
		for _, v := range decls.Env {
			if _, dup := seenVar[v.Name]; dup {
				continue
			}
			seenVar[v.Name] = struct{}{}
			merged.Env = append(merged.Env, v)
		}
		for _, id := range decls.Resources.Env {
			if _, dup := seenRes[id]; dup {
				continue
			}
			seenRes[id] = struct{}{}
			merged.Resources.Env = append(merged.Resources.Env, id)
		}
	}
	return merged, nil
}

// isYAML reports whether name has a YAML extension.
func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

// IsTestFile reports whether name is a test file rather than a config file:
// orders_test.yaml tests the flows in orders.yaml, the way orders_test.go tests
// orders.go. A test file lives beside the flows it exercises — that is the whole
// point of the convention — so the config loader has to walk past it, exactly as
// the Go compiler walks past a _test.go.
//
// Skipping it is not cosmetic. A test file parses into an *empty* config today,
// because the YAML decoder here ignores unknown keys, so it merges in silently and
// contributes nothing. That is harmless by luck, not by design: it means a config
// directory's contents depend on a decoder setting nobody would think to check, and
// the day the loader rejects unknown keys, every project with a test file beside its
// flows stops booting.
//
// This is the shared definition of the convention. dolphin discovers the files this
// skips, so the two must never disagree about which is which — a file octo loads as
// config and dolphin reads as a test suite would be a genuinely confusing bug.
func IsTestFile(name string) bool {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return strings.HasSuffix(base, TestFileSuffix)
}

// MergeConfigs combines multiple parsed configs into one: it concatenates
// connectors, processors, and flows, rejecting duplicate names so every
// connector, processor, and flow stays uniquely addressable. The service
// identity may be declared in at most one file.
func MergeConfigs(configs []types.Config) (types.Config, error) {
	m := newConfigMerger()
	for _, cfg := range configs {
		if err := m.add(cfg); err != nil {
			return types.Config{}, err
		}
	}
	return m.merged, nil
}

// configMerger accumulates configs while enforcing unique names per kind.
type configMerger struct {
	merged     types.Config
	serviceSet bool
	connectors map[string]struct{}
	processors map[string]struct{}
	flows      map[string]struct{}
	env        map[string]struct{}
	envRes     map[string]struct{}
	tplRes     map[string]struct{}
}

func newConfigMerger() *configMerger {
	return &configMerger{
		connectors: make(map[string]struct{}),
		processors: make(map[string]struct{}),
		flows:      make(map[string]struct{}),
		env:        make(map[string]struct{}),
		envRes:     make(map[string]struct{}),
		tplRes:     make(map[string]struct{}),
	}
}

// add folds one config into the merge, rejecting duplicate names and a second
// service declaration.
func (m *configMerger) add(cfg types.Config) error {
	if err := m.addService(cfg.Service); err != nil {
		return err
	}
	m.addEnv(cfg.Env)
	m.addResources(cfg.Resources)
	for _, c := range cfg.Connectors {
		if err := claimName(m.connectors, c.Name, "connector"); err != nil {
			return err
		}
		m.merged.Connectors = append(m.merged.Connectors, c)
	}
	for _, p := range cfg.Processors {
		if err := claimName(m.processors, p.Name, "processor"); err != nil {
			return err
		}
		m.merged.Processors = append(m.merged.Processors, p)
	}
	for _, f := range cfg.Flows {
		if f.Name != "" {
			if err := claimName(m.flows, f.Name, "flow"); err != nil {
				return err
			}
		}
		m.merged.Flows = append(m.merged.Flows, f)
	}
	return nil
}

// addEnv folds a config's declared env variables into the merge. Duplicate
// declarations across files are allowed (the same variable may be used in several
// configs); the first declaration wins.
func (m *configMerger) addEnv(env []types.EnvVar) {
	for _, e := range env {
		if _, dup := m.env[e.Name]; dup {
			continue
		}
		m.env[e.Name] = struct{}{}
		m.merged.Env = append(m.merged.Env, e)
	}
}

// addResources folds a config's declared resources (env + template resources) into
// the merge, de-duplicating so the same resource declared in several files folds to
// one. Env resources keep first-seen order (the combined load order is
// deterministic); templates dedup by their reference name — the alias, or the
// resource id when unaliased — so a repeated alias keeps its first declaration.
func (m *configMerger) addResources(res types.ResourcesConfig) {
	for _, id := range res.Env {
		if _, dup := m.envRes[id]; dup {
			continue
		}
		m.envRes[id] = struct{}{}
		m.merged.Resources.Env = append(m.merged.Resources.Env, id)
	}
	for _, t := range res.Templates {
		key := t.As
		if key == "" {
			key = t.Resource
		}
		if _, dup := m.tplRes[key]; dup {
			continue
		}
		m.tplRes[key] = struct{}{}
		m.merged.Resources.Templates = append(m.merged.Resources.Templates, t)
	}
}

// addService records the service identity, rejecting a second declaration.
func (m *configMerger) addService(svc types.ServiceConfig) error {
	if svc == (types.ServiceConfig{}) {
		return nil
	}
	if m.serviceSet {
		return fmt.Errorf("service identity is declared in more than one config file")
	}
	m.merged.Service = svc
	m.serviceSet = true
	return nil
}

// claimName records name in seen, erroring if it was already present. kind labels
// the error.
func claimName(seen map[string]struct{}, name, kind string) error {
	if _, dup := seen[name]; dup {
		return fmt.Errorf("%s %q is defined more than once", kind, name)
	}
	seen[name] = struct{}{}
	return nil
}
