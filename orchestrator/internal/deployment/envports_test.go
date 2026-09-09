package deployment

import (
	"reflect"
	"strconv"
	"testing"
)

// A flow with an HTTP source, spelled the way most definitions do: the connector's
// address left to the environment.
const httpSource = "flows:\n  - name: api\n    source:\n      connector: api\n      type: http\n"

func TestResolveRuntimeEnv(t *testing.T) {
	tests := []struct {
		name          string
		definition    string
		wantPort      int
		wantExposable bool
	}{
		// The port a definition names is the port it gets; naming none is fine too,
		// since the orchestrator is what supplies the value either way.
		{
			name:          "declared port with an http source",
			definition:    "env:\n  - name: HTTP_PORT\n    default: \"9090\"\nconnectors:\n  - name: api\n    type: http\n    settings:\n      port: ${HTTP_PORT}\n" + httpSource,
			wantPort:      9090,
			wantExposable: true,
		},
		{
			name:          "declared port and host",
			definition:    "env:\n  - name: HTTP_HOST\n    default: localhost\n  - name: HTTP_PORT\n    default: \"3000\"\nconnectors:\n  - name: api\n    type: http\n    settings:\n      host: ${HTTP_HOST}\n      port: ${HTTP_PORT}\n" + httpSource,
			wantPort:      3000,
			wantExposable: true,
		},
		// The drift this fixes: no env block, no connector, just a source. The
		// runtime resolves the connector on demand and it reads HTTP_PORT.
		{
			name:          "implicit connector from the source type",
			definition:    "flows:\n  - name: api\n    source:\n      type: http\n",
			wantPort:      defaultImplicitPort,
			wantExposable: true,
		},
		{
			name:          "implicit connector named by type in the binding",
			definition:    "flows:\n  - name: api\n    source:\n      connector: http\n      type: http\n",
			wantPort:      defaultImplicitPort,
			wantExposable: true,
		},
		{
			name:          "configured connector with no settings at all",
			definition:    "connectors:\n  - name: api\n    type: http\n" + httpSource,
			wantPort:      defaultImplicitPort,
			wantExposable: true,
		},
		{
			name:          "lone configured connector binds an unnamed source",
			definition:    "connectors:\n  - name: api\n    type: http\nflows:\n  - name: f\n    source:\n      type: http\n",
			wantPort:      defaultImplicitPort,
			wantExposable: true,
		},
		{
			name:          "settings other than the address do not pin it",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      basePath: /api/v1\n      requestTimeout: 5s\n" + httpSource,
			wantPort:      defaultImplicitPort,
			wantExposable: true,
		},
		// Pinning bind-all is what would have been injected anyway.
		{
			name:          "host pinned to bind-all is still reachable",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      host: 0.0.0.0\n" + httpSource,
			wantPort:      defaultImplicitPort,
			wantExposable: true,
		},

		// Everything below is a definition the injected address does not reach.

		// Settings beat the environment, so the pod would serve a port the Service
		// does not target — whether or not the env block declares HTTP_PORT.
		{
			name:          "connector pins its own port",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      port: 9000\n" + httpSource,
			wantExposable: false,
		},
		{
			name:          "connector pins its own port as a string",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      port: \"9000\"\n" + httpSource,
			wantExposable: false,
		},
		{
			name:          "declared HTTP_PORT that nothing reads",
			definition:    "env:\n  - name: HTTP_PORT\n    default: \"9090\"\nconnectors:\n  - name: api\n    type: http\n    settings:\n      port: 9000\n" + httpSource,
			wantExposable: false,
		},
		{
			name:          "os-assigned port is not routable",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      port: 0\n" + httpSource,
			wantExposable: false,
		},
		// Someone else's variable resolves to someone else's value.
		{
			name:          "port taken from another variable",
			definition:    "env:\n  - name: API_PORT\n    default: \"9000\"\nconnectors:\n  - name: api\n    type: http\n    settings:\n      port: ${API_PORT}\n" + httpSource,
			wantExposable: false,
		},
		// Referencing an undeclared variable is a load error: no listener at all.
		{
			name:          "port reference to an undeclared variable",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      port: ${HTTP_PORT}\n" + httpSource,
			wantExposable: false,
		},
		// The port is right and the pod still serves only itself.
		{
			name:          "host pinned to loopback",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      host: 127.0.0.1\n" + httpSource,
			wantExposable: false,
		},
		{
			name:          "host taken from an undeclared variable",
			definition:    "connectors:\n  - name: api\n    type: http\n    settings:\n      host: ${HTTP_HOST}\n" + httpSource,
			wantExposable: false,
		},
		// Both would take the injected port; the second one's Start fails on
		// "address already in use" and the run never comes up.
		{
			name:          "two env-bound connectors collide",
			definition:    "connectors:\n  - name: api\n    type: http\n  - name: admin\n    type: http\n" + httpSource,
			wantExposable: false,
		},
		// One of them owning its port leaves exactly one for the environment.
		{
			name:          "a second connector that pins its own port does not collide",
			definition:    "connectors:\n  - name: api\n    type: http\n  - name: admin\n    type: http\n    settings:\n      port: 9000\n" + httpSource,
			wantPort:      defaultImplicitPort,
			wantExposable: true,
		},
		{
			name:          "ambiguous binding is not exposable",
			definition:    "connectors:\n  - name: a\n    type: http\n  - name: b\n    type: http\n    settings:\n      port: 9000\nflows:\n  - name: f\n    source:\n      type: http\n",
			wantExposable: false,
		},
		// A listener with no routes answers 404; there is no endpoint to publish.
		{
			name:          "declared port with no http source",
			definition:    "env:\n  - name: HTTP_PORT\n    default: \"9090\"\n",
			wantExposable: false,
		},
		{
			name:          "configured connector with no http source",
			definition:    "connectors:\n  - name: api\n    type: http\nflows:\n  - name: f\n    source:\n      type: cron\n",
			wantExposable: false,
		},
		{
			name:          "no env",
			definition:    "service:\n  name: orders\n",
			wantExposable: false,
		},
		{
			name:          "sourceless flow is internal",
			definition:    "flows:\n  - name: f\n    process:\n      - type: log\n",
			wantExposable: false,
		},
		// A binding that names neither a configured instance nor the type does not
		// resolve in the runtime either — it fails to start rather than listening.
		{
			name:          "unresolvable binding is internal",
			definition:    "flows:\n  - name: f\n    source:\n      connector: nope\n      type: http\n",
			wantExposable: false,
		},
		// Two listeners, neither of them named. They are still two, and they still
		// take the same injected port.
		{
			name:          "two unnamed connectors collide",
			definition:    "connectors:\n  - type: http\n  - type: http\nflows:\n  - name: api\n    source:\n      type: http\n",
			wantExposable: false,
		},
		{
			name:          "malformed definition is internal",
			definition:    "flows: [this is not valid",
			wantExposable: false,
		},
		// Documents that parse but write a sequence as something else. The unmarshal
		// rejects them, and the editors' own parse has to agree — each of these
		// carries a source that would otherwise be exposable.
		{
			name:          "env written as a mapping is internal",
			definition:    "env:\n  A: 1\nflows:\n  - name: api\n    source:\n      type: http\n",
			wantExposable: false,
		},
		{
			name:          "connectors written as a mapping is internal",
			definition:    "connectors:\n  api:\n    type: http\nflows:\n  - name: api\n    source:\n      type: http\n",
			wantExposable: false,
		},
		{
			name:          "a list of scalars is internal",
			definition:    "env:\n  - HTTP_PORT\nflows:\n  - name: api\n    source:\n      type: http\n",
			wantExposable: false,
		},
		{
			name:          "flows written as a scalar is internal",
			definition:    "flows: nope\n",
			wantExposable: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, env, exposable := resolveRuntimeEnv(tt.definition)
			if exposable != tt.wantExposable {
				t.Fatalf("exposable = %v, want %v", exposable, tt.wantExposable)
			}
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
			if !tt.wantExposable {
				return
			}
			// An exposable definition is one whose address the orchestrator supplies,
			// so it supplies both halves of it.
			if env[envHTTPPort] != strconv.Itoa(tt.wantPort) {
				t.Errorf("%s = %q, want %d", envHTTPPort, env[envHTTPPort], tt.wantPort)
			}
			if env[envHTTPHost] != bindAllHost {
				t.Errorf("%s = %q, want %q (bind-all)", envHTTPHost, env[envHTTPHost], bindAllHost)
			}
		})
	}
}

func TestDeclaredEnvVars(t *testing.T) {
	tests := []struct {
		name       string
		definition string
		want       []EnvVarDecl
	}{
		{
			name: "excludes HTTP_PORT/HTTP_HOST, sorts, reads default+required",
			definition: "env:\n" +
				"  - name: HTTP_PORT\n    default: \"9090\"\n" +
				"  - name: LOG_LEVEL\n    default: info\n" +
				"  - name: API_KEY\n    required: true\n" +
				"  - name: HTTP_HOST\n    default: \"0.0.0.0\"\n",
			want: []EnvVarDecl{
				{Name: "API_KEY", Required: true},
				{Name: "LOG_LEVEL", Default: "info"},
			},
		},
		{
			name:       "no env",
			definition: "service:\n  name: orders\n",
			want:       []EnvVarDecl{},
		},
		{
			name:       "only orchestrator-managed vars yields none",
			definition: "env:\n  - name: HTTP_PORT\n    default: \"8080\"\n",
			want:       []EnvVarDecl{},
		},
		{
			name:       "malformed yaml yields nil",
			definition: "env: [this is not valid",
			want:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := declaredEnvVars(tt.definition)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("declaredEnvVars = %+v, want %+v", got, tt.want)
			}
		})
	}
}
