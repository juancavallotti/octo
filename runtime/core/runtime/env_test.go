package runtime

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func strptr(s string) *string { return &s }

func TestResolveEnvPrecedence(t *testing.T) {
	t.Setenv("DB_HOST", "from-os")
	decls := []types.EnvVar{
		{Name: "DB_HOST", Default: strptr("default-host")},
		{Name: "DB_PORT", Default: strptr("5432")}, // falls back to default
		{Name: "DB_USER"}, // supplied only by .env
	}
	dotenv := map[string]string{"DB_HOST": "from-dotenv", "DB_USER": "from-dotenv"}

	resolved, err := resolveEnv(decls, dotenv)
	if err != nil {
		t.Fatalf("resolveEnv: %v", err)
	}
	if got := resolved["DB_HOST"]; got != "from-os" {
		t.Errorf("DB_HOST = %q, want from-os (OS beats .env)", got)
	}
	if got := resolved["DB_PORT"]; got != "5432" {
		t.Errorf("DB_PORT = %q, want default 5432", got)
	}
	if got := resolved["DB_USER"]; got != "from-dotenv" {
		t.Errorf("DB_USER = %q, want from-dotenv", got)
	}
}

func TestResolveEnvRequired(t *testing.T) {
	decls := []types.EnvVar{{Name: "API_KEY", Required: true}}

	if _, err := resolveEnv(decls, nil); err == nil || !strings.Contains(err.Error(), "API_KEY") {
		t.Fatalf("err = %v, want required error naming API_KEY", err)
	}

	resolved, err := resolveEnv(decls, map[string]string{"API_KEY": "k"})
	if err != nil {
		t.Fatalf("resolveEnv with .env value: %v", err)
	}
	if resolved["API_KEY"] != "k" {
		t.Errorf("API_KEY = %q, want k", resolved["API_KEY"])
	}
}

func TestResolveEnvRequiredIgnoresDefault(t *testing.T) {
	// A default must not satisfy a required variable.
	decls := []types.EnvVar{{Name: "API_KEY", Required: true, Default: strptr("d")}}
	if _, err := resolveEnv(decls, nil); err == nil {
		t.Fatal("required var with only a default should error")
	}
}

func TestSubstituteUndeclared(t *testing.T) {
	cfg := &types.Config{
		Connectors: []types.ConnectorConfig{{Settings: types.Settings{"host": "${MISSING}"}}},
	}
	err := substituteConfig(cfg, map[string]string{}, map[string]struct{}{})
	if err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("err = %v, want undeclared error", err)
	}
}

func TestSubstituteDeclaredButUnresolved(t *testing.T) {
	cfg := &types.Config{
		Connectors: []types.ConnectorConfig{{Settings: types.Settings{"host": "${HOST}"}}},
	}
	declared := map[string]struct{}{"HOST": {}}
	err := substituteConfig(cfg, map[string]string{}, declared)
	if err == nil || !strings.Contains(err.Error(), "no default") {
		t.Fatalf("err = %v, want unresolved error", err)
	}
}

func TestSubstituteTypedAndEmbedded(t *testing.T) {
	cfg := &types.Config{
		Connectors: []types.ConnectorConfig{{Settings: types.Settings{
			"port":    "${PORT}",
			"debug":   "${DEBUG}",
			"address": "host:${PORT}",
			"nested": map[string]any{
				"key":  "${SECRET}",
				"list": []any{"${PORT}", "literal"},
			},
		}}},
	}
	resolved := map[string]string{"PORT": "8080", "DEBUG": "true", "SECRET": "s3cret"}
	declared := map[string]struct{}{"PORT": {}, "DEBUG": {}, "SECRET": {}}
	if err := substituteConfig(cfg, resolved, declared); err != nil {
		t.Fatalf("substituteConfig: %v", err)
	}

	got := cfg.Connectors[0].Settings
	if got["port"] != 8080 {
		t.Errorf("port = %#v, want int 8080", got["port"])
	}
	if got["debug"] != true {
		t.Errorf("debug = %#v, want bool true", got["debug"])
	}
	if got["address"] != "host:8080" {
		t.Errorf("address = %#v, want string host:8080", got["address"])
	}
	nested := got["nested"].(map[string]any)
	if nested["key"] != "s3cret" {
		t.Errorf("nested.key = %#v, want s3cret", nested["key"])
	}
	list := nested["list"].([]any)
	if list[0] != 8080 || list[1] != "literal" {
		t.Errorf("nested.list = %#v, want [8080 literal]", list)
	}
}

// TestCoerceOnlyMakesScalars: the substitution gives ${PORT} its natural int type, and
// that is ALL it may do. An environment variable is a string, and a good many ordinary
// strings are accidentally valid YAML for a structure — a SQLite DSN most of all:
//
//	DB_DSN=file::memory:   parses as the map {"file::memory": null}
//
// which reached the database connector as an object and was rejected with "cannot
// unmarshal object into ... dsn of type string" — a perfectly good DSN refused because
// it contained a colon. Anything that is not a scalar stays the string it came in as.
func TestCoerceOnlyMakesScalars(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  any
	}{
		{name: "an int fills an int setting", value: "8080", want: 8080},
		{name: "a bool fills a bool setting", value: "true", want: true},
		{name: "a float fills a float setting", value: "1.5", want: 1.5},
		{name: "an empty value stays the empty string", value: "", want: ""},
		{name: "a plain string stays a string", value: "s3cret", want: "s3cret"},
		// The regression. Each of these is valid YAML for something that is not a scalar.
		{name: "a sqlite memory dsn stays a string", value: "file::memory:", want: "file::memory:"},
		{name: "a sqlite file dsn stays a string", value: "file:orders.db", want: "file:orders.db"},
		{name: "a bracketed value stays a string", value: "[a, b]", want: "[a, b]"},
		{name: "a key-like value stays a string", value: "key: value", want: "key: value"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := coerce(tc.value); got != tc.want {
				t.Errorf("coerce(%q) = %#v, want %#v", tc.value, got, tc.want)
			}
		})
	}
}

// TestSubstituteKeepsADSNAString is the same regression, through the whole
// substitution, so it cannot come back by a different route than coerce.
func TestSubstituteKeepsADSNAString(t *testing.T) {
	cfg := &types.Config{
		Connectors: []types.ConnectorConfig{{Settings: types.Settings{"dsn": "${DB_DSN}"}}},
	}
	resolved := map[string]string{"DB_DSN": "file::memory:"}
	declared := map[string]struct{}{"DB_DSN": {}}

	if err := substituteConfig(cfg, resolved, declared); err != nil {
		t.Fatalf("substituteConfig: %v", err)
	}
	if got := cfg.Connectors[0].Settings["dsn"]; got != "file::memory:" {
		t.Errorf("dsn = %#v, want the string file::memory: — a DSN is not a map", got)
	}
}

func TestSubstituteNestedFlowBlocks(t *testing.T) {
	cfg := &types.Config{
		Flows: []types.FlowConfig{{
			Source: &types.SourceConfig{Settings: types.Settings{"path": "${PATH}"}},
			Process: []types.BlockConfig{{
				Type: "handle-errors",
				Process: []types.BlockConfig{
					{Type: "log", Settings: types.Settings{"level": "${LEVEL}"}},
				},
				Error: []types.BlockConfig{
					{Type: "log", Settings: types.Settings{"level": "${LEVEL}"}},
				},
			}},
		}},
	}
	resolved := map[string]string{"PATH": "/orders", "LEVEL": "info"}
	declared := map[string]struct{}{"PATH": {}, "LEVEL": {}}
	if err := substituteConfig(cfg, resolved, declared); err != nil {
		t.Fatalf("substituteConfig: %v", err)
	}
	if got := cfg.Flows[0].Source.Settings["path"]; got != "/orders" {
		t.Errorf("source path = %#v, want /orders", got)
	}
	level := cfg.Flows[0].Process[0].Process[0].Settings["level"]
	if level != "info" {
		t.Errorf("nested block level = %#v, want info", level)
	}
}

func TestParseConfigSubstitutesFromOSEnv(t *testing.T) {
	t.Setenv("HTTP_PORT", "9090")
	yaml := []byte(`
env:
  - name: HTTP_PORT
    default: "8080"
connectors:
  - name: api
    type: http
    settings:
      port: ${HTTP_PORT}
`)
	cfg, err := ParseConfig(yaml)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if got := cfg.Connectors[0].Settings["port"]; got != 9090 {
		t.Errorf("port = %#v, want int 9090", got)
	}
}
