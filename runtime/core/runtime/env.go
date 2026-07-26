package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/dotenv"
	"github.com/juancavallotti/octo/runtime/types"
)

// envFileVar names the environment variable holding an extra .env file path,
// loaded in addition to ./.env and overlaying it.
const envFileVar = "OCTO_ENV_FILE"

// defaultEnvFile is the .env path loaded relative to the working directory.
const defaultEnvFile = ".env"

// placeholderPattern matches a ${NAME} reference within a settings value.
var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// exactPlaceholder matches a value that is nothing but a single ${NAME} reference,
// so it can be substituted with the variable's native (typed) value rather than a
// string.
var exactPlaceholder = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// strTag is YAML's string tag. yaml.v3 keeps its own copy unexported, and a
// substituted node has to be able to say "this stayed a string" explicitly —
// leaving the tag off would hand the value back to the resolver, which is the
// opposite of what an unquoted DSN or date needs.
const strTag = "!!str"

// DotEnvPaths returns the .env files consulted during config loading, in load
// order: ./.env first, then $OCTO_ENV_FILE (if set) which overlays it. Paths are
// returned whether or not they exist so callers (e.g. the file watcher) can watch
// for a file being created later.
func DotEnvPaths() []string {
	paths := []string{defaultEnvFile}
	if extra := os.Getenv(envFileVar); extra != "" {
		paths = append(paths, extra)
	}
	return paths
}

// envDecls are the parts of a config that must be read before substitution can
// run: the variable declarations themselves, and the env resources that supply
// their values. They are decoded from the raw document, ahead of any rewriting,
// which is why both sections stay literal — a ${NAME} in either would have to
// resolve before the environment exists.
type envDecls struct {
	Env       []types.EnvVar        `yaml:"env"`
	Resources types.ResourcesConfig `yaml:"resources"`
}

// resolveDeclared resolves the declared variables of one or more config documents
// against the .env chain and the declared env resources, returning the resolved
// values and the set of declared names. decls arrive already merged across files,
// so a variable declared in one file serves a reference in another.
func resolveDeclared(decls envDecls, loader core.ResourceLoader) (map[string]string, map[string]struct{}, error) {
	dotenv, err := loadDotEnv()
	if err != nil {
		return nil, nil, err
	}
	// Overlay declared env resources on the built-in ./.env + $OCTO_ENV_FILE chain,
	// in listed order. A missing resource is skipped; one that fails to load or
	// parse is warned and skipped so the runtime falls back to the env it already
	// has. A genuinely missing required variable is still caught by resolveEnv below.
	for k, v := range loadEnvResources(loader, decls.Resources.Env) {
		dotenv[k] = v
	}
	resolved, err := resolveEnv(decls.Env, dotenv)
	if err != nil {
		return nil, nil, err
	}
	declared := make(map[string]struct{}, len(decls.Env))
	for _, decl := range decls.Env {
		declared[decl.Name] = struct{}{}
	}
	if len(declared) > 0 {
		slog.Info("resolved environment variables", "count", len(resolved), "declared", sortedKeys(declared))
	}
	return resolved, declared, nil
}

// loadDotEnv reads the .env files from DotEnvPaths, merging them so a later file
// overlays an earlier one. Missing files are skipped silently; a present file that
// fails to read or parse is an error.
func loadDotEnv() (map[string]string, error) {
	merged := make(map[string]string)
	for _, path := range DotEnvPaths() {
		data, err := os.ReadFile(path) //nolint:gosec // G304: operator-provided .env path
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read .env file %q: %w", path, err)
		}
		values, parseErr := dotenv.Parse(data)
		if parseErr != nil {
			return nil, fmt.Errorf("%q: %w", path, parseErr)
		}
		for k, v := range values {
			merged[k] = v
		}
		slog.Info("loaded .env file", "path", path, "variables", len(values))
	}
	return merged, nil
}

// loadEnvResources reads the declared env resources through loader and merges them
// so a later id overlays an earlier one. It never returns an error: a missing
// resource is skipped silently (a missing env file is never a runtime error), and
// a resource that fails to load or parse is logged and skipped so a broken import
// cannot take down the runtime as long as required variables are otherwise
// satisfied.
func loadEnvResources(loader core.ResourceLoader, ids []string) map[string]string {
	merged := make(map[string]string)
	ctx := context.Background()
	for _, id := range ids {
		data, err := loader.Load(ctx, core.ResourceKindEnv, id)
		if err != nil {
			if !errors.Is(err, core.ErrResourceNotFound) {
				slog.Warn("skipping env resource that failed to load", "id", id, "error", err)
			}
			continue
		}
		values, parseErr := dotenv.Parse(data)
		if parseErr != nil {
			slog.Warn("skipping env resource that failed to parse", "id", id, "error", parseErr)
			continue
		}
		for k, v := range values {
			merged[k] = v
		}
		slog.Info("loaded env resource", "id", id, "variables", len(values))
	}
	return merged
}

// resolveEnv resolves each declared variable to a value using the precedence OS
// environment > .env file > declared default. A variable marked required must be
// supplied by the OS environment or a .env file; a default does not satisfy it.
// The returned map holds only variables that resolved to a value.
func resolveEnv(decls []types.EnvVar, dotenv map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(decls))
	for _, decl := range decls {
		if decl.Name == "" {
			return nil, fmt.Errorf("env declaration requires a name")
		}
		value, supplied := lookupEnv(decl.Name, dotenv)
		switch {
		case supplied:
			resolved[decl.Name] = value
		case decl.Required:
			return nil, fmt.Errorf("required environment variable %q is not set", decl.Name)
		case decl.Default != nil:
			resolved[decl.Name] = *decl.Default
		}
	}
	return resolved, nil
}

// lookupEnv finds a variable's externally supplied value, preferring the OS
// environment over the .env file. supplied reports whether either provided it.
func lookupEnv(name string, dotenv map[string]string) (value string, supplied bool) {
	if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	if v, ok := dotenv[name]; ok {
		return v, true
	}
	return "", false
}

// literalSections are the top-level keys substitution leaves alone. Both are read
// off the raw document to build the environment in the first place, so a ${NAME}
// in either would have to resolve before there is anything to resolve it with.
var literalSections = map[string]struct{}{"env": {}, "resources": {}}

// substituteDocument rewrites every ${NAME} reference in a parsed config
// document, in place, before it is decoded into typed fields. Rewriting the node
// tree rather than the decoded config is what lets a reference fill a field of
// any type: workers: ${FLOW_WORKERS} is a string when the decoder sees it, so
// substituting afterwards was always too late for anything but a settings map.
func substituteDocument(doc *yaml.Node, resolved map[string]string, declared map[string]struct{}) error {
	if doc == nil {
		return nil
	}
	s := &substitutor{resolved: resolved, declared: declared}
	return s.node(doc, true)
}

// substitutor carries the resolved values and declared-name set through the
// document walk.
type substitutor struct {
	resolved map[string]string
	declared map[string]struct{}
}

// node substitutes references in every scalar value at or below n. topLevel marks
// the document's own mapping, whose literalSections are skipped.
//
// Mapping keys are left alone: a substituted key could collide with a sibling, and
// no key is a place a variable belongs. Aliases are left alone too — the anchor
// they point at is itself part of the tree and gets substituted there, so
// following them would rewrite the same scalar twice.
func (s *substitutor) node(n *yaml.Node, topLevel bool) error {
	switch n.Kind {
	case yaml.ScalarNode:
		return s.scalar(n)
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if topLevel {
				if _, literal := literalSections[n.Content[i].Value]; literal {
					continue
				}
			}
			if err := s.node(n.Content[i+1], false); err != nil {
				return err
			}
		}
		return nil
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			// A document's single child is the top-level mapping.
			if err := s.node(child, topLevel && n.Kind == yaml.DocumentNode); err != nil {
				return err
			}
		}
		return nil
	default: // yaml.AliasNode, or a zero node from an empty document
		return nil
	}
}

// scalar substitutes references in a single scalar node, in place.
//
// A value that is exactly one ${NAME} takes the variable's natural type: clearing
// the node's tag and quoting style hands it back to the decoder to resolve, so
// ${PORT} can fill an int field. Whether the author quoted the placeholder makes
// no difference, matching how this behaved when it ran over decoded settings.
// Anything coerce leaves a string — an empty value, a DSN like file::memory:, a
// date — is pinned to !!str so it decodes as the string it already was. An
// embedded reference is textual and stays a string either way.
func (s *substitutor) scalar(n *yaml.Node) error {
	if m := exactPlaceholder.FindStringSubmatch(n.Value); m != nil {
		value, err := s.resolve(m[1])
		if err != nil {
			return err
		}
		n.Value = value
		if _, isString := coerce(value).(string); isString {
			n.Tag = strTag
		} else {
			n.Tag, n.Style = "", 0
		}
		return nil
	}

	var subErr error
	out := placeholderPattern.ReplaceAllStringFunc(n.Value, func(match string) string {
		name := placeholderPattern.FindStringSubmatch(match)[1]
		value, err := s.resolve(name)
		if err != nil {
			subErr = err
			return match
		}
		return value
	})
	if subErr != nil {
		return subErr
	}
	n.Value = out
	return nil
}

// resolve returns a referenced variable's value, erroring when the name is
// undeclared or declared but resolved to no value (and has no default).
func (s *substitutor) resolve(name string) (string, error) {
	if _, ok := s.declared[name]; !ok {
		return "", fmt.Errorf("config references undeclared environment variable %q (add it under env:)", name)
	}
	value, ok := s.resolved[name]
	if !ok {
		return "", fmt.Errorf("environment variable %q is referenced but not set and has no default", name)
	}
	return value, nil
}

// coerce converts a resolved string into its natural YAML SCALAR type (int, bool, or
// float), so e.g. ${PORT} can fill an int setting. An empty value stays the empty
// string rather than becoming nil.
//
// Scalars only, and that is the whole point of the type switch below. An environment
// variable is a string; the coercion exists to spare an int setting from receiving
// "8080", not to let a value become a structure. And plenty of ordinary strings are
// accidentally valid YAML for one:
//
//	DB_DSN=file::memory:        parses as the MAP {"file::memory": null}
//
// which reached the database connector as an object and failed to decode — a real DSN,
// rejected because it happened to contain a colon. Anything that does not parse as a
// scalar is left as the string it already was.
func coerce(value string) any {
	if value == "" {
		return ""
	}
	var out any
	if err := yaml.Unmarshal([]byte(value), &out); err != nil || out == nil {
		return value
	}
	switch out.(type) {
	case bool, int, int64, float64:
		return out
	default:
		return value
	}
}

// sortedKeys returns the keys of set in sorted order, for stable log output.
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
