package runtime

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func strptr(s string) *string { return &s }

// parseYAML runs a config document through the whole load path — declarations
// resolved, ${NAME} substituted, then decoded. Substitution rewrites the document
// before it is typed, so there is no longer a decoded config to substitute into:
// these tests exercise it the way the runtime does, from source.
func parseYAML(t *testing.T, doc string) (types.Config, error) {
	t.Helper()
	return ParseConfig([]byte(doc))
}

func mustParseYAML(t *testing.T, doc string) types.Config {
	t.Helper()
	cfg, err := parseYAML(t, doc)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return cfg
}

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
	_, err := parseYAML(t, `
connectors:
  - name: api
    type: http
    settings:
      host: ${MISSING}
`)
	if err == nil || !strings.Contains(err.Error(), "undeclared") {
		t.Fatalf("err = %v, want undeclared error", err)
	}
}

func TestSubstituteDeclaredButUnresolved(t *testing.T) {
	_, err := parseYAML(t, `
env:
  - name: HOST
connectors:
  - name: api
    type: http
    settings:
      host: ${HOST}
`)
	if err == nil || !strings.Contains(err.Error(), "no default") {
		t.Fatalf("err = %v, want unresolved error", err)
	}
}

func TestSubstituteTypedAndEmbedded(t *testing.T) {
	cfg := mustParseYAML(t, `
env:
  - name: PORT
    default: "8080"
  - name: DEBUG
    default: "true"
  - name: SECRET
    default: s3cret
connectors:
  - name: api
    type: http
    settings:
      port: ${PORT}
      debug: ${DEBUG}
      address: host:${PORT}
      nested:
        key: ${SECRET}
        list:
          - ${PORT}
          - literal
`)

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
	// The decoder gives a nested untyped map the outer map's type, so this is a
	// types.Settings rather than a bare map[string]any.
	nested := got["nested"].(types.Settings)
	if nested["key"] != "s3cret" {
		t.Errorf("nested.key = %#v, want s3cret", nested["key"])
	}
	list := nested["list"].([]any)
	if list[0] != 8080 || list[1] != "literal" {
		t.Errorf("nested.list = %#v, want [8080 literal]", list)
	}
}

// TestSubstituteQuotedPlaceholderStillTypes: whether the author quoted the
// placeholder makes no difference to the result. Quoting is how you write a
// placeholder that YAML would otherwise mangle, not a request for a string, and
// this behaved the same when substitution ran over already-decoded settings —
// where the quotes were long gone by the time it looked.
func TestSubstituteQuotedPlaceholderStillTypes(t *testing.T) {
	cfg := mustParseYAML(t, `
env:
  - name: PORT
    default: "8080"
connectors:
  - name: api
    type: http
    settings:
      port: "${PORT}"
`)
	if got := cfg.Connectors[0].Settings["port"]; got != 8080 {
		t.Errorf("port = %#v, want int 8080", got)
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

// TestSubstituteKeepsValuesUntyped is the same regression, through the whole
// substitution, so it cannot come back by a different route than coerce. It
// matters more now that substitution rewrites YAML: a value handed back to the
// decoder untagged would be re-resolved, and each of these resolves to something
// that is not the string it started as.
func TestSubstituteKeepsValuesUntyped(t *testing.T) {
	cfg := mustParseYAML(t, `
env:
  - name: DB_DSN
    default: "file::memory:"
  - name: EMPTY
    default: ""
  - name: RELEASED_ON
    default: "2026-07-25"
connectors:
  - name: db
    type: database
    settings:
      dsn: ${DB_DSN}
      empty: ${EMPTY}
      releasedOn: ${RELEASED_ON}
`)
	settings := cfg.Connectors[0].Settings
	if got := settings["dsn"]; got != "file::memory:" {
		t.Errorf("dsn = %#v, want the string file::memory: — a DSN is not a map", got)
	}
	if got := settings["empty"]; got != "" {
		t.Errorf("empty = %#v, want the empty string, not nil", got)
	}
	if got := settings["releasedOn"]; got != "2026-07-25" {
		t.Errorf("releasedOn = %#v, want the string 2026-07-25, not a timestamp", got)
	}
}

func TestSubstituteNestedFlowBlocks(t *testing.T) {
	cfg := mustParseYAML(t, `
env:
  - name: PATH_
    default: /orders
  - name: LEVEL
    default: info
flows:
  - name: orders
    source:
      connector: api
      type: http
      settings:
        path: ${PATH_}
    process:
      - type: handle-errors
        process:
          - type: log
            settings:
              level: ${LEVEL}
        error:
          - type: log
            settings:
              level: ${LEVEL}
`)
	if got := cfg.Flows[0].Source.Settings["path"]; got != "/orders" {
		t.Errorf("source path = %#v, want /orders", got)
	}
	level := cfg.Flows[0].Process[0].Process[0].Settings["level"]
	if level != "info" {
		t.Errorf("nested block level = %#v, want info", level)
	}
}

// TestSubstituteRootFlowScalars is the reported bug: workers is exactly the
// setting you want to differ between a laptop and a production node, and it is
// typed, so it never survived to the settings walk — the load died in the decoder
// with `cannot unmarshal !!str into int`. buffer and pool are the same shape.
func TestSubstituteRootFlowScalars(t *testing.T) {
	t.Setenv("FLOW_POOL", "24")
	cfg := mustParseYAML(t, `
env:
  - name: FLOW_WORKERS
    default: "64"
  - name: FLOW_BUFFER
    default: "128"
  - name: FLOW_POOL
    default: "8"
flows:
  - name: orders
    workers: ${FLOW_WORKERS}
    buffer: ${FLOW_BUFFER}
    pool: ${FLOW_POOL}
    process:
      - type: log
`)
	flow := cfg.Flows[0]
	if flow.Workers != 64 {
		t.Errorf("workers = %d, want 64 (from the declared default)", flow.Workers)
	}
	if flow.Buffer != 128 {
		t.Errorf("buffer = %d, want 128", flow.Buffer)
	}
	if flow.Pool != 24 {
		t.Errorf("pool = %d, want 24 (the OS environment beats the default)", flow.Pool)
	}
}

// TestSubstituteReachesNonSettingsScalars: substitution now runs over the whole
// document, so a reference works wherever a value does — not only under settings.
func TestSubstituteReachesNonSettingsScalars(t *testing.T) {
	cfg := mustParseYAML(t, `
env:
  - name: SERVICE_NAME
    default: orders-service
  - name: MIN_TOTAL
    default: "100"
service:
  name: ${SERVICE_NAME}
flows:
  - name: orders
    process:
      - type: if
        condition: body.total > ${MIN_TOTAL}
        then:
          process:
            - type: log
`)
	if got := cfg.Service.Name; got != "orders-service" {
		t.Errorf("service.name = %q, want orders-service", got)
	}
	if got := cfg.Flows[0].Process[0].Condition; got != "body.total > 100" {
		t.Errorf("condition = %q, want body.total > 100", got)
	}
}

// TestEnvDeclarationsStayLiteral: the env and resources sections are read to build
// the environment in the first place, so a reference in either would have to
// resolve before there is anything to resolve it with. They are left alone.
func TestEnvDeclarationsStayLiteral(t *testing.T) {
	cfg := mustParseYAML(t, `
env:
  - name: GREETING
    default: "${NOT_A_REFERENCE}"
`)
	if got := cfg.Env[0].Default; got == nil || *got != "${NOT_A_REFERENCE}" {
		t.Errorf("default = %v, want the literal ${NOT_A_REFERENCE}", got)
	}
}

func TestParseConfigSubstitutesFromOSEnv(t *testing.T) {
	t.Setenv("HTTP_PORT", "9090")
	cfg := mustParseYAML(t, `
env:
  - name: HTTP_PORT
    default: "8080"
connectors:
  - name: api
    type: http
    settings:
      port: ${HTTP_PORT}
`)
	if got := cfg.Connectors[0].Settings["port"]; got != 9090 {
		t.Errorf("port = %#v, want int 9090", got)
	}
}
