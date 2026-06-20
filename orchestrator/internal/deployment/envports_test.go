package deployment

import (
	"reflect"
	"testing"
)

func TestResolveRuntimeEnv(t *testing.T) {
	tests := []struct {
		name          string
		definition    string
		wantPort      int
		wantExposable bool
		wantHost      bool // HTTP_HOST present in the supplied env
	}{
		{
			name:          "port only",
			definition:    "env:\n  - name: HTTP_PORT\n    default: \"9090\"\n",
			wantPort:      9090,
			wantExposable: true,
		},
		{
			name:          "port and host",
			definition:    "env:\n  - name: HTTP_HOST\n    default: localhost\n  - name: HTTP_PORT\n    default: \"3000\"\n",
			wantPort:      3000,
			wantExposable: true,
			wantHost:      true,
		},
		{
			name:          "no env",
			definition:    "service:\n  name: orders\n",
			wantExposable: false,
		},
		{
			name:          "port without default",
			definition:    "env:\n  - name: HTTP_PORT\n    required: true\n",
			wantExposable: false,
		},
		{
			name:          "non-numeric default",
			definition:    "env:\n  - name: HTTP_PORT\n    default: nope\n",
			wantExposable: false,
		},
		{
			name:          "out of range default",
			definition:    "env:\n  - name: HTTP_PORT\n    default: \"70000\"\n",
			wantExposable: false,
		},
		{
			name:          "host only is not exposable",
			definition:    "env:\n  - name: HTTP_HOST\n    default: \"0.0.0.0\"\n",
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
			if env[envHTTPPort] == "" {
				t.Errorf("supplied env is missing %s: %v", envHTTPPort, env)
			}
			if _, ok := env[envHTTPHost]; ok != tt.wantHost {
				t.Errorf("env has %s = %v, want present=%v", envHTTPHost, env[envHTTPHost], tt.wantHost)
			}
			if tt.wantHost && env[envHTTPHost] != bindAllHost {
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
