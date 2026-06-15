package runtime

import (
	"strings"
	"testing"

	"github.com/juancavallotti/eip-go/types"
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

func TestSubstituteNestedFlowBlocks(t *testing.T) {
	cfg := &types.Config{
		Flows: []types.FlowConfig{{
			Source: &types.SourceConfig{Settings: types.Settings{"path": "${PATH}"}},
			Process: []types.BlockConfig{{
				Type: "scope",
				Main: &types.FlowConfig{Process: []types.BlockConfig{
					{Type: "log", Settings: types.Settings{"level": "${LEVEL}"}},
				}},
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
	level := cfg.Flows[0].Process[0].Main.Process[0].Settings["level"]
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
