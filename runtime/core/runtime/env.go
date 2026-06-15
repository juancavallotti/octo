package runtime

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/eip-go/core/internal/dsl"
	"github.com/juancavallotti/eip-go/types"
)

// envFileVar names the environment variable holding an extra .env file path,
// loaded in addition to ./.env and overlaying it.
const envFileVar = "EIP_ENV_FILE"

// defaultEnvFile is the .env path loaded relative to the working directory.
const defaultEnvFile = ".env"

// placeholderPattern matches a ${NAME} reference within a settings value.
var placeholderPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// exactPlaceholder matches a value that is nothing but a single ${NAME} reference,
// so it can be substituted with the variable's native (typed) value rather than a
// string.
var exactPlaceholder = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// DotEnvPaths returns the .env files consulted during config loading, in load
// order: ./.env first, then $EIP_ENV_FILE (if set) which overlays it. Paths are
// returned whether or not they exist so callers (e.g. the file watcher) can watch
// for a file being created later.
func DotEnvPaths() []string {
	paths := []string{defaultEnvFile}
	if extra := os.Getenv(envFileVar); extra != "" {
		paths = append(paths, extra)
	}
	return paths
}

// applyEnv resolves the config's declared environment variables and substitutes
// ${NAME} references throughout its settings. It is a no-op for a config with no
// env declarations and no references.
func applyEnv(cfg *types.Config) error {
	dotenv, err := loadDotEnv()
	if err != nil {
		return err
	}
	resolved, err := resolveEnv(cfg.Env, dotenv)
	if err != nil {
		return err
	}
	declared := make(map[string]struct{}, len(cfg.Env))
	for _, decl := range cfg.Env {
		declared[decl.Name] = struct{}{}
	}
	if len(declared) > 0 {
		slog.Info("resolved environment variables", "count", len(resolved), "declared", sortedKeys(declared))
	}
	return substituteConfig(cfg, resolved, declared)
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
		values, parseErr := dsl.ParseDotEnv(data)
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

// substituteConfig rewrites every ${NAME} reference in the config's settings.
func substituteConfig(cfg *types.Config, resolved map[string]string, declared map[string]struct{}) error {
	s := &substitutor{resolved: resolved, declared: declared}
	for i := range cfg.Connectors {
		if err := s.settings(cfg.Connectors[i].Settings); err != nil {
			return err
		}
	}
	for i := range cfg.Processors {
		if err := s.settings(cfg.Processors[i].Settings); err != nil {
			return err
		}
	}
	for i := range cfg.Flows {
		if err := s.flow(&cfg.Flows[i]); err != nil {
			return err
		}
	}
	return nil
}

// substitutor carries the resolved values and declared-name set through the config
// walk.
type substitutor struct {
	resolved map[string]string
	declared map[string]struct{}
}

// flow substitutes references in a flow's source and every block, recursing into
// the sub-flows a composite block embeds.
func (s *substitutor) flow(cfg *types.FlowConfig) error {
	if cfg == nil {
		return nil
	}
	if cfg.Source != nil {
		if err := s.settings(cfg.Source.Settings); err != nil {
			return err
		}
	}
	for i := range cfg.Process {
		if err := s.block(&cfg.Process[i]); err != nil {
			return err
		}
	}
	return nil
}

// block substitutes references in a block's settings and any sub-flows it carries.
// The sub-flow slots mirror BlockConfig's composite fields.
func (s *substitutor) block(cfg *types.BlockConfig) error {
	if err := s.settings(cfg.Settings); err != nil {
		return err
	}
	subFlows := []*types.FlowConfig{cfg.Main, cfg.Alternative, cfg.Then, cfg.Else, cfg.Default, cfg.Body}
	for i := range cfg.Branches {
		subFlows = append(subFlows, &cfg.Branches[i])
	}
	for i := range cfg.Cases {
		subFlows = append(subFlows, &cfg.Cases[i].Flow)
	}
	for _, sub := range subFlows {
		if err := s.flow(sub); err != nil {
			return err
		}
	}
	return nil
}

// settings substitutes references in every value of a settings map, in place.
func (s *substitutor) settings(set types.Settings) error {
	for k, v := range set {
		nv, err := s.value(v)
		if err != nil {
			return err
		}
		set[k] = nv
	}
	return nil
}

// value substitutes references in a settings value, recursing into nested maps and
// slices. A value that is exactly one ${NAME} reference is replaced with the
// variable's native type; an embedded reference is replaced textually (staying a
// string).
func (s *substitutor) value(v any) (any, error) {
	switch val := v.(type) {
	case string:
		return s.scalar(val)
	case types.Settings:
		return val, s.settings(val)
	case map[string]any:
		return val, s.settings(val)
	case []any:
		for i := range val {
			nv, err := s.value(val[i])
			if err != nil {
				return nil, err
			}
			val[i] = nv
		}
		return val, nil
	default:
		return v, nil
	}
}

// scalar substitutes references in a single string value.
func (s *substitutor) scalar(str string) (any, error) {
	if m := exactPlaceholder.FindStringSubmatch(str); m != nil {
		value, err := s.resolve(m[1])
		if err != nil {
			return nil, err
		}
		return coerce(value), nil
	}

	var subErr error
	out := placeholderPattern.ReplaceAllStringFunc(str, func(match string) string {
		name := placeholderPattern.FindStringSubmatch(match)[1]
		value, err := s.resolve(name)
		if err != nil {
			subErr = err
			return match
		}
		return value
	})
	if subErr != nil {
		return nil, subErr
	}
	return out, nil
}

// resolve returns a referenced variable's value, erroring when the name is
// undeclared or declared but resolved to no value (and has no default).
func (s *substitutor) resolve(name string) (string, error) {
	if _, ok := s.declared[name]; !ok {
		return "", fmt.Errorf("settings reference undeclared environment variable %q (add it under env:)", name)
	}
	value, ok := s.resolved[name]
	if !ok {
		return "", fmt.Errorf("environment variable %q is referenced but not set and has no default", name)
	}
	return value, nil
}

// coerce converts a resolved string into its natural YAML scalar type (int, bool,
// float, or string), so e.g. ${PORT} can fill an int setting. An empty value stays
// the empty string rather than becoming nil.
func coerce(value string) any {
	if value == "" {
		return ""
	}
	var out any
	if err := yaml.Unmarshal([]byte(value), &out); err != nil || out == nil {
		return value
	}
	return out
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
