package deployment

import "testing"

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
